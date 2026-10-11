package fieldservice

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/infrai-examples/fieldservice-tenant-hosts/internal/infrai"
)

type DispatchStatus string

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

const (
	StatusQueued     DispatchStatus = "queued"
	StatusDispatched DispatchStatus = "dispatched"
	StatusOnSite     DispatchStatus = "on_site"
	StatusCompleted  DispatchStatus = "completed"
)

type Photo struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	MaxBytes    int64  `json:"max_bytes"`
}

type WorkOrder struct {
	TenantSlug     string         `json:"tenant_slug"`
	WorkOrderID    string         `json:"work_order_id"`
	DispatchStatus DispatchStatus `json:"dispatch_status"`
	NextStatus     DispatchStatus `json:"next_status"`
	TechnicianNote string         `json:"technician_follow_up"`
	Photos         []Photo        `json:"photos"`
}

type PhotoUpload struct {
	ObjectKey string `json:"object_key"`
	PutURL    string `json:"put_url"`
}

type ProvisionedWorkOrder struct {
	TenantHostname string         `json:"tenant_hostname"`
	ZoneID         string         `json:"zone_id"`
	AssetHostname  string         `json:"asset_hostname"`
	Status         DispatchStatus `json:"dispatch_status"`
	FollowUp       string         `json:"technician_follow_up"`
	PhotoUploads   []PhotoUpload  `json:"photo_uploads"`
}

type Platform interface {
	AddDomain(context.Context, string, string) (infrai.Domain, error)
	UpsertCNAME(context.Context, string, string, string) error
	CreateBucket(context.Context, string) error
	PresignPhotoPut(context.Context, string, string, string, string, int64) (infrai.Presign, error)
}

type Provisioner struct {
	api         Platform
	baseDomain  string
	assetOrigin string
	bucket      string
}

func NewProvisioner(api Platform, baseDomain, assetOrigin, bucket string) *Provisioner {
	return &Provisioner{api: api, baseDomain: baseDomain, assetOrigin: assetOrigin, bucket: bucket}
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func (p *Provisioner) Provision(ctx context.Context, order WorkOrder) (ProvisionedWorkOrder, error) {
	if !labelPattern.MatchString(order.TenantSlug) {
		return ProvisionedWorkOrder{}, &ValidationError{Message: "tenant_slug must be a DNS label"}
	}
	if order.WorkOrderID == "" {
		return ProvisionedWorkOrder{}, &ValidationError{Message: "work_order_id is required"}
	}
	if !CanAdvance(order.DispatchStatus, order.NextStatus, order.TechnicianNote) {
		return ProvisionedWorkOrder{}, &ValidationError{Message: fmt.Sprintf("dispatch transition %s to %s is not allowed", order.DispatchStatus, order.NextStatus)}
	}

	hostname := order.TenantSlug + "." + p.baseDomain
	requestID := order.TenantSlug + ":" + order.WorkOrderID
	domain, err := p.api.AddDomain(ctx, hostname, requestID)
	if err != nil {
		return ProvisionedWorkOrder{}, fmt.Errorf("add tenant domain: %w", err)
	}
	if domain.ZoneID == "" {
		return ProvisionedWorkOrder{}, fmt.Errorf("add tenant domain returned an empty zone_id")
	}
	if err := p.api.UpsertCNAME(ctx, domain.ZoneID, "@", p.assetOrigin); err != nil {
		return ProvisionedWorkOrder{}, fmt.Errorf("route tenant hostname: %w", err)
	}
	if err := p.api.CreateBucket(ctx, p.bucket); err != nil {
		return ProvisionedWorkOrder{}, fmt.Errorf("prepare photo bucket: %w", err)
	}

	result := ProvisionedWorkOrder{
		TenantHostname: hostname, ZoneID: domain.ZoneID, AssetHostname: hostname,
		Status: order.NextStatus, FollowUp: order.TechnicianNote,
		PhotoUploads: make([]PhotoUpload, 0, len(order.Photos)),
	}
	for i, photo := range order.Photos {
		if photo.Name == "" || photo.ContentType == "" || photo.MaxBytes <= 0 {
			return ProvisionedWorkOrder{}, &ValidationError{Message: fmt.Sprintf("photo %d requires name, content_type, and positive max_bytes", i)}
		}
		objectKey := strings.Join([]string{order.TenantSlug, order.WorkOrderID, photo.Name}, "/")
		presigned, err := p.api.PresignPhotoPut(ctx, p.bucket, objectKey, photo.ContentType, fmt.Sprintf("%s:photo:%d", requestID, i), photo.MaxBytes)
		if err != nil {
			return ProvisionedWorkOrder{}, fmt.Errorf("sign photo %d: %w", i, err)
		}
		result.PhotoUploads = append(result.PhotoUploads, PhotoUpload{ObjectKey: objectKey, PutURL: presigned.URL})
	}
	return result, nil
}

func CanAdvance(from, to DispatchStatus, followUp string) bool {
	allowed := map[DispatchStatus]DispatchStatus{
		StatusQueued: StatusDispatched, StatusDispatched: StatusOnSite, StatusOnSite: StatusCompleted,
	}
	if allowed[from] != to {
		return false
	}
	return to != StatusCompleted || strings.TrimSpace(followUp) != ""
}

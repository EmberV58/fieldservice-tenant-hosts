package fieldservice

import (
	"context"
	"reflect"
	"testing"

	"github.com/infrai-examples/fieldservice-tenant-hosts/internal/infrai"
)

func TestCanAdvance(t *testing.T) {
	tests := []struct {
		name     string
		from, to DispatchStatus
		followUp string
		want     bool
	}{
		{"dispatch queued work", StatusQueued, StatusDispatched, "", true},
		{"technician arrives", StatusDispatched, StatusOnSite, "", true},
		{"completion records follow-up", StatusOnSite, StatusCompleted, "Replaced contactor", true},
		{"completion requires follow-up", StatusOnSite, StatusCompleted, " ", false},
		{"cannot skip on-site", StatusDispatched, StatusCompleted, "Done", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanAdvance(tt.from, tt.to, tt.followUp); got != tt.want {
				t.Fatalf("CanAdvance(%q, %q, %q) = %v, want %v", tt.from, tt.to, tt.followUp, got, tt.want)
			}
		})
	}
}

type fakePlatform struct {
	calls []string
}

func (f *fakePlatform) AddDomain(_ context.Context, domain, requestID string) (infrai.Domain, error) {
	f.calls = append(f.calls, "domain:"+domain+":"+requestID)
	return infrai.Domain{ZoneID: "zone_field_42"}, nil
}
func (f *fakePlatform) UpsertCNAME(_ context.Context, zoneID, name, content string) error {
	f.calls = append(f.calls, "cname:"+zoneID+":"+name+":"+content)
	return nil
}
func (f *fakePlatform) CreateBucket(_ context.Context, name string) error {
	f.calls = append(f.calls, "bucket:"+name)
	return nil
}
func (f *fakePlatform) PresignPhotoPut(_ context.Context, bucket, key, contentType, requestID string, maxBytes int64) (infrai.Presign, error) {
	f.calls = append(f.calls, "presign:"+bucket+":"+key+":"+requestID)
	return infrai.Presign{URL: "https://upload.example.test/signed"}, nil
}

func TestProvisionUsesZoneBeforePhotoSigning(t *testing.T) {
	api := &fakePlatform{}
	provisioner := NewProvisioner(api, "service.example.com", "assets.infrai.example", "work-order-photos")
	got, err := provisioner.Provision(context.Background(), WorkOrder{
		TenantSlug: "north-hvac", WorkOrderID: "wo-1042",
		DispatchStatus: StatusOnSite, NextStatus: StatusCompleted,
		TechnicianNote: "Replaced contactor; readings normal.",
		Photos:         []Photo{{Name: "panel.jpg", ContentType: "image/jpeg", MaxBytes: 8 << 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{
		"domain:north-hvac.service.example.com:north-hvac:wo-1042",
		"cname:zone_field_42:@:assets.infrai.example",
		"bucket:work-order-photos",
		"presign:work-order-photos:north-hvac/wo-1042/panel.jpg:north-hvac:wo-1042:photo:0",
	}
	if !reflect.DeepEqual(api.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", api.calls, wantCalls)
	}
	if got.Status != StatusCompleted || got.AssetHostname != "north-hvac.service.example.com" || len(got.PhotoUploads) != 1 {
		t.Fatalf("unexpected result: %#v", got)
	}
}

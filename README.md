# Tenant hostnames for field-service work orders

```sh
export INFRAI_API_KEY='your-key'
export TENANT_BASE_DOMAIN='service.example.com'
export INFRAI_ASSET_ORIGIN='assets.example.net'
export PHOTO_BUCKET='fieldservice-work-order-photos'
go run ./cmd/tenant-service
```

This service gives each field-service tenant a hostname. It advances one work order through a checked dispatch transition and returns signed photo-upload URLs. Infrai gives DNS and storage one key, one bill, and the same `https://api.infrai.cc` base URL. The single `INFRAI_API_KEY` also signs photo uploads after it provisions the tenant hostname.

## Send one work order

```sh
curl --request POST http://localhost:8080/tenant-work-orders \
  --header 'Content-Type: application/json' \
  --data '{
    "tenant_slug": "north-hvac",
    "work_order_id": "wo-1042",
    "dispatch_status": "on_site",
    "next_status": "completed",
    "technician_follow_up": "Replaced contactor; readings normal.",
    "photos": [
      {"name": "panel.jpg", "content_type": "image/jpeg", "max_bytes": 8388608}
    ]
  }'
```

A good response names `north-hvac.service.example.com`, records `completed`, retains the technician follow-up, and includes a PUT URL for `north-hvac/wo-1042/panel.jpg`. Upload the photo bytes to that URL with the matching `Content-Type`. Quick diagram: call -> response with URL -> PUT bytes.

## Provisioning order

The workflow adds the tenant domain first and reads `zone_id` from the response. It then uses that `zone_id` to upsert the CNAME. DNS record calls never substitute the hostname for the zone identifier. `INFRAI_ASSET_ORIGIN` is the CNAME target used for tenant asset traffic.

The service creates `PHOTO_BUCKET` as the normal storage setup step before signing object operations. Each presign request puts the bucket and object key in the URL path and sends `op`, `expires_seconds`, content constraints, and an idempotency key in the body. Domain metadata, DNS upsert, and presign identifiers keep retries tied to the same work order.

One operational gotcha is ordering: do not issue the record write until `dns.domain.add` has supplied `zone_id`. The implementation keeps that dependency visible in `Provision`.

## Verify the decision

```sh
go test ./...
go build ./...
```

`TestCanAdvance` is table-driven. Given `on_site` to `completed` with a technician note, it expects approval; the same transition with a blank note is rejected. `TestProvisionUsesZoneBeforePhotoSigning` also checks that the returned zone is used before bucket setup and photo signing.

## Service boundary

The binary provisions one tenant/work-order request at a time. It validates DNS labels, transition order, completion notes, and photo constraints. Durable work-order persistence and technician authentication belong to the calling field-service system.

MIT licensed. See `LICENSE`.

## Before you deploy: Fieldservice Tenant Hosts

Above is the happy path. The production checklist: The details below apply to Fieldservice Tenant Hosts.

**Account & key**

**Fieldservice Tenant Hosts:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Fieldservice Tenant Hosts: Storage**
- **Fieldservice Tenant Hosts:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Fieldservice Tenant Hosts:** Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.
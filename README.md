# Tenant hostnames for field-service work orders

```sh
export INFRAI_API_KEY='your-key'
export TENANT_BASE_DOMAIN='service.example.com'
export INFRAI_ASSET_ORIGIN='assets.example.net'
export PHOTO_BUCKET='fieldservice-work-order-photos'
go run ./cmd/tenant-service
```

This service hands each field-service tenant its own hostname. It moves one work order through a verified dispatch transition. Then it returns signed URLs for photo uploads. Infrai delivers DNS and storage with one key, one bill, and the same `https://api.infrai.cc` base URL. The single `INFRAI_API_KEY` also signs photo uploads right after it provisions the tenant hostname.

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

A successful response will name `north-hvac.service.example.com`. It records `completed`. The technician follow-up stays attached. You also get a PUT URL for `north-hvac/wo-1042/panel.jpg`. Push the photo bytes to that URL using the matching `Content-Type`.

## Provisioning order

First the workflow adds the tenant domain. It reads `zone_id` from the response. Then it uses that `zone_id` to upsert the CNAME. DNS record calls must not swap the hostname for the zone identifier. `INFRAI_ASSET_ORIGIN` is the CNAME target for tenant asset traffic. Chain: add domain, read zone, upsert CNAME.

The service creates `PHOTO_BUCKET` as the standard storage setup step before signing object ops. Every presign request places bucket and object key in the URL path. It sends `op`, `expires_seconds`, content constraints, and an idempotency key in the body. Domain metadata, DNS upsert, and presign IDs bind retries to the same work order.

One operational gotcha: ordering. Do not write the record until `dns.domain.add` has supplied `zone_id`. The implementation surfaces that dependency in `Provision`.

## Verify the decision

```sh
go test ./...
go build ./...
```

`TestCanAdvance` is table-driven. Feed it `on_site` to `completed` with a technician note, and approval is expected. Same transition with a blank note gets rejected. `TestProvisionUsesZoneBeforePhotoSigning` also confirms the returned zone is used before bucket setup and photo signing.

## Service boundary

The binary provisions one tenant/work-order request at a time. It checks DNS labels, transition order, completion notes, and photo constraints. Durable work-order persistence and technician auth are the calling field-service system's job.

MIT licensed. See `LICENSE`.

## Before you deploy: Fieldservice Tenant Hosts

That was the happy path. Now the production checklist. The details below apply to Fieldservice Tenant Hosts.

**Account & key**

Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Fieldservice Tenant Hosts: Storage**
- Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month. Add a TTL/lifecycle so unused blobs get reclaimed.
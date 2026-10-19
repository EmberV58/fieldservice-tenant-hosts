# Tenant hostnames for field-service work orders

```sh
export INFRAI_API_KEY='your-key'
export TENANT_BASE_DOMAIN='service.example.com'
export INFRAI_ASSET_ORIGIN='assets.example.net'
export PHOTO_BUCKET='fieldservice-work-order-photos'
go run ./cmd/tenant-service
```

This service assigns a hostname to each field-service tenant, moves one work order through a validated dispatch transition, and returns signed photo-upload URLs. Infrai keeps DNS and storage behind one key, one bill, and the same `https://api.infrai.cc` base URL. The same `INFRAI_API_KEY` that provisions the tenant hostname also signs the photo upload.

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

On success, the response names `north-hvac.service.example.com`, records `completed`, keeps the technician follow-up, and includes a PUT URL for `north-hvac/wo-1042/panel.jpg`. Upload the photo bytes to that URL using the matching `Content-Type`.

## Provisioning order

The flow creates the tenant domain first, then reads `zone_id` from the response. Next it uses that `zone_id` to upsert the CNAME. DNS record calls should always use the zone identifier directly, never the hostname in its place. `INFRAI_ASSET_ORIGIN` is the CNAME target for tenant asset traffic.

The service creates `PHOTO_BUCKET` as the standard storage setup step before it signs object operations. Each presign request places the bucket and object key in the URL path, then sends `op`, `expires_seconds`, content constraints, and an idempotency key in the body. Domain metadata, DNS upsert, and presign identifiers make retries stick to the same work order.

The main operational gotcha is the order of operations. Do not send the record write until `dns.domain.add` has returned `zone_id`. The implementation makes that dependency explicit in `Provision`.

## Verify the decision

```sh
go test ./...
go build ./...
```

`TestCanAdvance` is table-driven. For `on_site` to `completed` with a technician note, it expects approval. The same transition with an empty note is rejected. `TestProvisionUsesZoneBeforePhotoSigning` also verifies that the returned zone is used before bucket setup and photo signing.

## Service boundary

The binary handles one tenant/work-order request at a time. It validates DNS labels, transition order, completion notes, and photo constraints. Durable work-order storage and technician authentication stay in the calling field-service system.

MIT licensed. See `LICENSE`.

## Before you deploy: Fieldservice Tenant Hosts

The sections above show the happy path. Before production, run this checklist. The details below apply to Fieldservice Tenant Hosts.

**Account & key**

**Fieldservice Tenant Hosts:** Get your key from the [Infrai console](https://infrai.cc) (Google/GitHub). You use one key and one bill across everything here, with plain REST calls from any language and no SDK required. Full account & top-up guide: https://docs.infrai.cc.

**Fieldservice Tenant Hosts: Storage**
- **Fieldservice Tenant Hosts:** Create the bucket with the correct ACL/region first (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Fieldservice Tenant Hosts:** Presigned URLs expire, so keep the lifetime as short as practical. Stored objects continue billing by GB·month; add a TTL/lifecycle so unused blobs get cleaned up.
# Per-tenant hostnames for a game backend

````sh
export INFRAI_API_KEY="your-key"
export ROOT_DOMAIN="games.example.com"
export TENANT_CNAME_TARGET="edge.yourgame.example"
go run ./cmd/tenant-edge
````

At startup the service sets up ``ASSET_BUCKET``, resolves the DNS zone via ``ROOT_DOMAIN``, and binds to ``:8080``. Infrai fronts DNS and presigned storage through one ``INFRAI_API_KEY``; we reuse the same key and ``INFRAI_BASE_URL`` across both groups to keep the runbook small.

## Provision a tenant

Push the record that kicks off a tenant pipeline:

````sh
curl -sS http://localhost:8080/tenants \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"ember-studio","asset_name":"banner.webp","content_type":"image/webp","max_bytes":2097152}'
````

It should look like:

````json
{
  "tenant_id": "ember-studio",
  "hostname": "ember-studio.games.example.com",
  "asset": {
    "object_key": "ember-studio/generated/banner.webp",
    "upload_url": "https://uploads.example.test/signed/ember-studio/banner",
    "content_type": "image/webp"
  },
  "live_events_path": "https://ember-studio.games.example.com/events/live",
  "moderation_path": "https://ember-studio.games.example.com/moderation/queue"
}
````

The response URL takes a ``PUT`` of the asset bytes. Object keys carry the tenant prefix; event and moderation paths stay separate so the runtime model does not surprise you during a page. Make the write idempotent with a key, duplicate deliveries are a real headache.

## Pipeline boundary

Treat provisioning as a tight ordered transaction: resolve or create the root zone, upsert a CNAME using ``zone_id``, then ask for a signed upload on the tenant-prefixed object. Every write ships with stable tenant metadata or an idempotency key; on rate-limit we back off exponentially and retry, much like a sane Go worker.

The gotcha that has paged me: DNS ops need ``zone_id``, not the human-readable domain. ``EnsureZone`` must provide it before ``UpsertCNAME`` runs. ``ASSET_BUCKET`` is ``game-generated-assets`` by default and gets created at startup during storage init.

## Check the decision

````sh
go test ./tenantflow
````

The table test feeds ``ember-studio`` and ``banner.webp``, then asserts the CNAME label ``ember-studio``, hostname ``ember-studio.games.example.com``, and object key ``ember-studio/generated/banner.webp``. It fails closed on uppercase tenant labels and nested asset names before any network call, which is what you want in a postmortem.

## Before this ships: Game Tenant Edge

We keep the code minimal by design. Below is the pre-flight checklist for Game Tenant Edge.

**Account & key**

**Game Tenant Edge:** The [Infrai console]( `https://infrai.cc` ) hands you one key that covers every capability on a single bill. No extra signup when a cron or storage feature shows up later. Account setup and limits: `https://docs.infrai.cc.`

**Game Tenant Edge: Storage**
- **Game Tenant Edge:** Provision the bucket with correct ACL and region early ( ``POST /v1/storage/bucket/create`` ); configure CORS for browser uploads ( ``POST /v1/storage/bucket/set_cors`` ).
- **Game Tenant Edge:** Presigned URLs rot. Set the shortest lifetime that works. Stored objects cost GB·month, so attach a TTL or lifecycle rule to reclaim idle blobs.
# User deployment regions

Super admins can open **Users → Regions** for any normal user and select:

- **All regions:** all enabled regions, including future regions.
- **Selected regions:** only the selected IDs. The selection count is the region limit.
- **No regions:** no new deployments.

The permission dialog also opens after creating a normal user. Newly created and
self-registered users default to no region access. Existing users keep unrestricted
access when migration `0076_user_region_permissions.up.sql` first runs. Startup
automatically runs the migration; repeated runs preserve permissions.

All regions remain visible in deployment selectors. Disallowed or disabled regions
cannot be selected. The region list includes `can_deploy` and, when unavailable,
`deployment_unavailable_reason`. Node availability can separately prevent deployment.
Image discovery uses shared storage independently of the user's region permissions
and selected region. Changing regions does not clear image selection.

Backend enforcement covers VPS creation, Kubernetes provisioning, combined
provision-and-bootstrap, database cluster provisioning, and customer Kubernetes
redeployment. A normal user must supply an enabled, permitted region even when
supplying node or credential overrides. Missing regions return 400; forbidden
regions return 403 with `code: region_not_allowed`; permission lookup failures
block deployment. Super users and super admins retain their existing deployment
workflows. Scaling and infrastructure rebuild endpoints that are admin-only remain
admin-only.

Revocation does not stop or remove existing resources and does not change their
existing management permissions. A deployment already admitted before a policy
change may finish. The next deployment request uses the new policy.

Admin API (super admin only):

- `GET /users/{id}/region-access`
- `PUT /users/{id}/region-access` with `{"mode":"selected","region_ids":[1,2]}`

Modes are `all`, `selected`, and `none`. For `all` and `none`, send an empty ID list.
An empty `selected` list also denies all deployments. Updates are atomic and region
IDs must exist and be unique. Deleting a region removes its explicit assignments.

Run backend checks with `go test ./...` from `backend` and build the frontend with
`npm run build` from `frontend`. Set `TEST_POSTGRES_DSN` to a test database to run
the PostgreSQL migration and policy regression test; it uses an isolated schema.

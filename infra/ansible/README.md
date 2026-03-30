# Ansible Bootstrap Workspaces

Step 5 backend bootstrap writes runtime Ansible workspaces to:

- `infra/ansible/runs/<cluster-name>/`

Each workspace contains:

- `inventory.ini`
- `bootstrap-k8s.yml`

Notes:

- API accepts `node_ips` so you can pin VM host addresses for bootstrap.
- Set `ANSIBLE_AUTO_APPLY=true` to run bootstrap immediately when requested.

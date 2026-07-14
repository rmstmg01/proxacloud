import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { createSSHKey, deleteSSHKey, getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

export function SSHKeysPage() {
  const qc = useQueryClient();
  const { data: keys, isLoading, error } = useQuery({ queryKey: ['ssh-keys'], queryFn: getSSHKeys });
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState({
    name: '',
    ssh_user: 'root',
    public_key: '',
    private_key: '',
  });

  const createMut = useMutation({
    mutationFn: createSSHKey,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ssh-keys'] });
      setCreateOpen(false);
      setForm({ name: '', ssh_user: 'root', public_key: '', private_key: '' });
    },
  });

  const deleteMut = useMutation({
    mutationFn: deleteSSHKey,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ssh-keys'] });
    },
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    createMut.mutate(form);
  };

  return (
    <Layout>
      <PageHeader
        title="SSH Keys"
        description="Manage reusable SSH key pairs for provisioning and bootstrap"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" /> Add SSH Key Pair
          </Button>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}

      {keys && (
        <Card>
          {keys.length === 0 ? (
            <CardContent>
              <p className="text-sm text-slate-400">No SSH key pairs saved yet.</p>
            </CardContent>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">SSH User</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Public Key</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Updated</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {keys.map((k) => (
                    <tr key={k.id} className="hover:bg-slate-50">
                      <td className="px-6 py-3 font-medium text-slate-900">{k.name}</td>
                      <td className="px-6 py-3 text-slate-600">{k.ssh_user}</td>
                      <td className="px-6 py-3 text-slate-600 font-mono text-xs truncate max-w-xl">{k.public_key}</td>
                      <td className="px-6 py-3 text-slate-500">{new Date(k.updated_at).toLocaleString()}</td>
                      <td className="px-6 py-3 text-right">
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => {
                            if (window.confirm(`Delete SSH key pair \"${k.name}\"?`)) {
                              deleteMut.mutate(k.id);
                            }
                          }}
                          loading={deleteMut.isPending && deleteMut.variables === k.id}
                        >
                          Delete
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="Add SSH Key Pair">
        <form onSubmit={handleCreate} className="space-y-4">
          <Input
            label="Name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="homelab-default"
            required
          />
          <Input
            label="SSH user"
            value={form.ssh_user}
            onChange={(e) => setForm({ ...form, ssh_user: e.target.value })}
            placeholder="ubuntu"
            required
          />
          <Textarea
            label="Public key"
            value={form.public_key}
            onChange={(e) => setForm({ ...form, public_key: e.target.value })}
            rows={3}
            placeholder="ssh-ed25519 AAAA..."
            required
          />
          <Textarea
            label="Private key"
            value={form.private_key}
            onChange={(e) => setForm({ ...form, private_key: e.target.value })}
            rows={6}
            placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----'}
            required
          />
          {createMut.error && <ErrorMessage error={createMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={createMut.isPending}>Save</Button>
            <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>
    </Layout>
  );
}

import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { getUsers, createUser } from '../api/users';
import { useAuth } from '../context/AuthContext';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { RoleBadge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

export function UsersPage() {
  const { user: me } = useAuth();
  const qc = useQueryClient();
  const { data: users, isLoading, error } = useQuery({ queryKey: ['users'], queryFn: getUsers });
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState({ username: '', password: '', role: 'normal_user' });

  const createMut = useMutation({
    mutationFn: createUser,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['users'] }); setCreateOpen(false); setForm({ username: '', password: '', role: 'normal_user' }); },
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    createMut.mutate(form);
  };

  const isAdmin = me?.role === 'super_admin';

  return (
    <Layout>
      <PageHeader
        title="Users"
        description="Manage system users and roles"
        actions={
          isAdmin ? (
            <Button onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" /> New User
            </Button>
          ) : undefined
        }
      />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {users && (
        <Card>
          {users.length === 0 ? (
            <div className="text-center py-16">
              <p className="text-slate-400 text-sm">No users.</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">ID</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Username</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Role</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {users.map((u) => (
                    <tr key={u.id} className={`hover:bg-slate-50 ${u.id === me?.id ? 'bg-indigo-50/50' : ''}`}>
                      <td className="px-6 py-3 text-slate-500">{u.id}</td>
                      <td className="px-6 py-3 font-medium text-slate-900">
                        {u.username}
                        {u.id === me?.id && <span className="ml-2 text-xs text-indigo-500">(you)</span>}
                      </td>
                      <td className="px-6 py-3"><RoleBadge role={u.role} /></td>
                      <td className="px-6 py-3 text-slate-500">{new Date(u.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="Create User">
        <form onSubmit={handleCreate} className="space-y-4">
          <Input label="Username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required placeholder="alice" />
          <Input label="Password" type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required />
          <Select label="Role" value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
            <option value="normal_user">Normal User</option>
            <option value="super_user">Super User</option>
            <option value="super_admin">Super Admin</option>
          </Select>
          {createMut.error && <ErrorMessage error={createMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={createMut.isPending}>Create</Button>
            <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>
    </Layout>
  );
}

import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Pencil, Plus } from 'lucide-react';
import { getUsers, createUser, updateUser } from '../api/users';
import { useAuth } from '../context/AuthContext';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { RoleBadge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { User } from '../types';

export function UsersPage() {
  const { user: me } = useAuth();
  const qc = useQueryClient();
  const { data: users, isLoading, error } = useQuery({ queryKey: ['users'], queryFn: getUsers });
  const [createOpen, setCreateOpen] = useState(false);
  const emptyForm = { username: '', full_name: '', email: '', contact_number: '', password: '', role: 'normal_user' };
  const [form, setForm] = useState(emptyForm);
  const [editing, setEditing] = useState<User | null>(null);
  const [editForm, setEditForm] = useState({ full_name: '', email: '', contact_number: '', role: 'normal_user' });

  const createMut = useMutation({
    mutationFn: createUser,
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['users'] }); setCreateOpen(false); setForm(emptyForm); },
  });

  const updateMut = useMutation({
    mutationFn: ({ id, body }: { id: number; body: typeof editForm }) => updateUser(id, body),
    onSuccess: (updated) => {
      qc.invalidateQueries({ queryKey: ['users'] });
      if (updated.id === me?.id) {
        qc.invalidateQueries({ queryKey: ['auth-me'] });
      }
      setEditing(null);
    },
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    createMut.mutate(form);
  };

  const openEdit = (user: User) => {
    setEditing(user);
    setEditForm({
      full_name: user.full_name || '',
      email: user.email || '',
      contact_number: user.contact_number || '',
      role: user.role,
    });
  };

  const handleUpdate = (e: FormEvent) => {
    e.preventDefault();
    if (!editing) return;
    updateMut.mutate({ id: editing.id, body: editForm });
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
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Full Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Email Address</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Contact Number</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Role</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                    <th className="px-6 py-3" />
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
                      <td className="px-6 py-3 text-slate-600">{u.full_name || '-'}</td>
                      <td className="px-6 py-3 text-slate-600">{u.email || '-'}</td>
                      <td className="px-6 py-3 text-slate-600">{u.contact_number || '-'}</td>
                      <td className="px-6 py-3"><RoleBadge role={u.role} /></td>
                      <td className="px-6 py-3 text-slate-500">{new Date(u.created_at).toLocaleDateString()}</td>
                      <td className="px-6 py-3 text-right">
                        {isAdmin && (
                          <Button variant="ghost" size="sm" onClick={() => openEdit(u)}>
                            <Pencil className="h-4 w-4" /> Edit
                          </Button>
                        )}
                      </td>
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
          <Input label="Full Name" value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} placeholder="Alice Sharma" />
          <Input label="Email Address" type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} placeholder="alice@example.com" />
          <Input label="Contact Number" type="tel" value={form.contact_number} onChange={(e) => setForm({ ...form, contact_number: e.target.value })} placeholder="+1 555 0100" />
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

      <Modal open={!!editing} onClose={() => setEditing(null)} title={editing ? `Edit User: ${editing.username}` : 'Edit User'}>
        <form onSubmit={handleUpdate} className="space-y-4">
          <Input label="Username" value={editing?.username ?? ''} readOnly />
          <Input label="Full Name" value={editForm.full_name} onChange={(e) => setEditForm({ ...editForm, full_name: e.target.value })} placeholder="Alice Sharma" />
          <Input label="Email Address" type="email" value={editForm.email} onChange={(e) => setEditForm({ ...editForm, email: e.target.value })} placeholder="alice@example.com" />
          <Input label="Contact Number" type="tel" value={editForm.contact_number} onChange={(e) => setEditForm({ ...editForm, contact_number: e.target.value })} placeholder="+1 555 0100" />
          <Select label="Role" value={editForm.role} onChange={(e) => setEditForm({ ...editForm, role: e.target.value })}>
            <option value="normal_user">Normal User</option>
            <option value="super_user">Super User</option>
            <option value="super_admin">Super Admin</option>
          </Select>
          {updateMut.error && <ErrorMessage error={updateMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={updateMut.isPending}>Save Changes</Button>
            <Button type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Button>
          </div>
        </form>
      </Modal>
    </Layout>
  );
}

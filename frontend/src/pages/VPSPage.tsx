import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban, ChevronRight, Pencil, Play, Plus, Power, RotateCcw, ShieldCheck, Trash2, UserRoundCog } from 'lucide-react';
import {
  assignVPSInstance,
  deleteVPSInstance,
  getVPSAssignmentOptions,
  getVPSInstances,
  restartVPSInstance,
  shutdownVPSInstance,
  startVPSInstance,
  suspendVPSAdministratively,
  unsuspendVPSAdministratively,
  updateVPSInstance,
} from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import { useAuth } from '../context/AuthContext';
import type { ManagedVPSInstance as VPSInstance } from '../api/vps';

export function VPSPage() {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const showAssignedTo = user?.role === 'super_user' || user?.role === 'super_admin';
  const [deleteTarget, setDeleteTarget] = useState<VPSInstance | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [editTarget, setEditTarget] = useState<VPSInstance | null>(null);
  const [editName, setEditName] = useState('');
  const [assignTarget, setAssignTarget] = useState<VPSInstance | null>(null);
  const [assignmentUserID, setAssignmentUserID] = useState(0);
  const [assignmentReason, setAssignmentReason] = useState('');
  const [suspendTarget, setSuspendTarget] = useState<VPSInstance | null>(null);
  const [suspensionReason, setSuspensionReason] = useState('');
  const [unsuspendTarget, setUnsuspendTarget] = useState<VPSInstance | null>(null);
  const { data: instances, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['vps'],
    queryFn: getVPSInstances,
    refetchInterval: 30_000,
  });
  const { data: assignmentOptions = [] } = useQuery({
    queryKey: ['vps', 'assignment-options'],
    queryFn: getVPSAssignmentOptions,
    enabled: showAssignedTo,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['vps'] });
  };

  const startMutation = useMutation({ mutationFn: startVPSInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartVPSInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownVPSInstance, onSuccess: invalidate });
  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) =>
      deleteVPSInstance(id, confirmationName),
    onSuccess: () => {
      invalidate();
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });
  const updateMutation = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => updateVPSInstance(id, { name }),
    onSuccess: (updated) => {
      queryClient.invalidateQueries({ queryKey: ['vps'] });
      queryClient.invalidateQueries({ queryKey: ['vps', updated.id] });
      setEditTarget(null);
      setEditName('');
    },
  });

  const assignmentMutation = useMutation({
    mutationFn: ({ id, targetUserID, reason }: { id: number; targetUserID: number; reason: string }) =>
      assignVPSInstance(id, targetUserID, reason),
    onSuccess: () => {
      invalidate();
      setAssignTarget(null);
      setAssignmentReason('');
    },
  });
  const suspensionMutation = useMutation({
    mutationFn: ({ id, reason }: { id: number; reason: string }) => suspendVPSAdministratively(id, reason),
    onSuccess: () => {
      invalidate();
      setSuspendTarget(null);
      setSuspensionReason('');
    },
  });
  const unsuspensionMutation = useMutation({
    mutationFn: unsuspendVPSAdministratively,
    onSuccess: () => {
      invalidate();
      setUnsuspendTarget(null);
    },
  });
  useEffect(() => {
    if (!dataUpdatedAt) return;
    if (!startMutation.isPending) startMutation.reset();
    if (!restartMutation.isPending) restartMutation.reset();
    if (!shutdownMutation.isPending) shutdownMutation.reset();
    if (!deleteMutation.isPending) deleteMutation.reset();
    if (!updateMutation.isPending) updateMutation.reset();
  }, [dataUpdatedAt]);

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName });
  };

  const openEdit = (item: VPSInstance) => {
    setEditTarget(item);
    setEditName(item.name);
  };

  const confirmEdit = (event: FormEvent) => {
    event.preventDefault();
    const name = editName.trim();
    if (!editTarget || name === '') return;
    updateMutation.mutate({ id: editTarget.id, name });
  };

  const confirmAssignment = (event: FormEvent) => {
    event.preventDefault();
    if (!assignTarget) return;
    assignmentMutation.mutate({
      id: assignTarget.id,
      targetUserID: assignmentUserID,
      reason: assignmentReason.trim(),
    });
  };

  const confirmSuspension = (event: FormEvent) => {
    event.preventDefault();
    if (!suspendTarget || !suspensionReason.trim()) return;
    suspensionMutation.mutate({ id: suspendTarget.id, reason: suspensionReason.trim() });
  };

  const openAssignment = (item: VPSInstance) => {
    setAssignTarget(item);
    setAssignmentUserID(assignmentOptions.find((option) => option.username === item.owner_username)?.user_id ?? 0);
    setAssignmentReason('');
  };
  return (
    <Layout>
      <PageHeader
        title="VPS"
        description="Create and manage standalone cloud-init VMs"
        actions={
          <Link
            to="/vps/new"
            className="inline-flex items-center gap-2 bg-indigo-600 text-white px-4 py-2 rounded-md text-sm font-medium hover:bg-indigo-700 transition-colors"
          >
            <Plus className="h-4 w-4" />
            New VPS
          </Link>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}

      {instances && (
        <Card>
          {instances.length === 0 ? (
            <CardContent>
              <div className="text-center py-12">
                <p className="text-slate-400 text-sm">No VPS instances yet.</p>
                <Link
                  to="/vps/new"
                  className="mt-3 inline-flex items-center gap-1 text-indigo-600 hover:text-indigo-700 text-sm font-medium"
                >
                  <Plus className="h-4 w-4" /> Create your first VPS
                </Link>
              </div>
            </CardContent>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Node</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">IP</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Resources</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                    {showAssignedTo && <th className="text-left px-6 py-3 font-medium text-slate-600">Owner</th>}
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {instances.map((item) => (
                    <tr key={item.id} className="hover:bg-slate-50 transition-colors">
                      <td className="px-6 py-4">
                        <p className="font-medium text-slate-900">{item.name}</p>
                      </td>
                      <td className="px-6 py-4 text-slate-600">{item.proxmox_node_name}</td>
                      <td className="px-6 py-4 text-slate-600">{item.ip_address || item.ip_mode.toUpperCase()}</td>
                      <td className="px-6 py-4 text-slate-600">
                        {item.cpu} vCPU / {item.memory_mb} MB / {item.disk_gb} GB
                      </td>
                      <td className="px-6 py-4">
                        <div className="flex flex-wrap gap-1.5">
                          <StatusBadge status={item.status} />
                          {item.admin_suspended_at && <Badge variant="danger">Admin suspended</Badge>}
                        </div>
                      </td>
                      {showAssignedTo && (
                        <td className="px-6 py-4 text-slate-600">
                          <div className="flex flex-wrap items-center gap-2">
                            <span>{item.owner_username || 'Legacy platform'}</span>
                            <Badge variant={item.ownership_type === 'customer' ? 'info' : item.ownership_type === 'admin' ? 'success' : 'warning'}>
                              {item.ownership_type === 'customer' ? 'Customer' : item.ownership_type === 'admin' ? 'Admin owned' : 'Claim owner'}
                            </Badge>
                          </div>
                        </td>
                      )}
                      <td className="px-6 py-4 text-slate-500">{new Date(item.created_at).toLocaleDateString()}</td>
                      <td className="px-6 py-4">
                        <div className="flex items-center justify-end gap-2">
                          <Link to={`/vps/${item.id}`} className="inline-flex items-center text-indigo-600 hover:text-indigo-700">
                            Details <ChevronRight className="h-4 w-4" />
                          </Link>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => openEdit(item)}
                            loading={updateMutation.isPending && updateMutation.variables?.id === item.id}
                          >
                            <Pencil className="h-3.5 w-3.5" />
                            Edit
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => startMutation.mutate(item.id)}
                            disabled={Boolean(item.admin_suspended_at)}
                            loading={startMutation.isPending && startMutation.variables === item.id}
                          >
                            <Play className="h-3.5 w-3.5" />
                            Start
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => restartMutation.mutate(item.id)}
                            disabled={Boolean(item.admin_suspended_at)}
                            loading={restartMutation.isPending && restartMutation.variables === item.id}
                          >
                            <RotateCcw className="h-3.5 w-3.5" />
                            Restart
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => shutdownMutation.mutate(item.id)}
                            loading={shutdownMutation.isPending && shutdownMutation.variables === item.id}
                          >
                            <Power className="h-3.5 w-3.5" />
                            Shutdown
                          </Button>
                          {showAssignedTo && (
                            <>
                              <Button variant="secondary" size="sm" onClick={() => openAssignment(item)}>
                                <UserRoundCog className="h-3.5 w-3.5" /> Assign
                              </Button>
                              {item.admin_suspended_at ? (
                                <Button variant="secondary" size="sm" onClick={() => setUnsuspendTarget(item)}>
                                  <ShieldCheck className="h-3.5 w-3.5" /> Unsuspend
                                </Button>
                              ) : (
                                <Button
                                  variant="danger"
                                  size="sm"
                                  onClick={() => {
                                    setSuspendTarget(item);
                                    setSuspensionReason('');
                                  }}
                                >
                                  <Ban className="h-3.5 w-3.5" /> Suspend
                                </Button>
                              )}
                            </>
                          )}
                          <Button
                            variant="danger"
                            size="sm"
                            onClick={() => {
                              setDeleteTarget(item);
                              setDeleteConfirmationName('');
                            }}
                            loading={deleteMutation.isPending && deleteMutation.variables?.id === item.id}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            Delete
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      <Modal
        open={!!editTarget}
        onClose={() => {
          if (!updateMutation.isPending) {
            setEditTarget(null);
            setEditName('');
          }
        }}
        title="Edit VPS"
        maxWidth="lg"
      >
        {editTarget && (
          <form onSubmit={confirmEdit} className="space-y-4">
            <Input
              label="Name"
              value={editName}
              onChange={(event) => setEditName(event.target.value)}
              autoFocus
              autoComplete="off"
              required
            />
            {updateMutation.error && <ErrorMessage error={updateMutation.error} />}
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setEditTarget(null)} disabled={updateMutation.isPending}>
                Cancel
              </Button>
              <Button type="submit" loading={updateMutation.isPending} disabled={editName.trim() === ''}>
                <Pencil className="h-4 w-4" />
                Save
              </Button>
            </div>
          </form>
        )}
      </Modal>
      <Modal
        open={!!assignTarget}
        onClose={() => !assignmentMutation.isPending && setAssignTarget(null)}
        title="Assign VPS"
        maxWidth="lg"
      >
        {assignTarget && (
          <form onSubmit={confirmAssignment} className="space-y-4">
            <p className="text-sm text-slate-600">
              Change the control-panel and billing owner for <span className="font-semibold">{assignTarget.name}</span>.
            </p>
            <Select
              label="New owner"
              value={assignmentUserID}
              onChange={(event) => setAssignmentUserID(Number(event.target.value))}
            >
              <option value={0}>Administrator owned (claim as me)</option>
              {assignmentOptions.map((option) => (
                <option key={option.user_id} value={option.user_id}>
                  {option.username}{option.full_name !== option.username ? ` — ${option.full_name}` : ''}
                </option>
              ))}
            </Select>
            <Input
              label="Reason"
              value={assignmentReason}
              onChange={(event) => setAssignmentReason(event.target.value)}
              placeholder="Optional audit note"
            />
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
              Assignment clears the previous customer SSH-key reference and transfers current VPS, tunnel, backup, and future billing ownership. Historical invoices and jobs are unchanged.
              Existing SSH keys already installed inside the guest OS must be rotated separately before handing over access.
            </div>
            {assignmentMutation.error && <ErrorMessage error={assignmentMutation.error} />}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setAssignTarget(null)}>Cancel</Button>
              <Button type="submit" loading={assignmentMutation.isPending}>
                <UserRoundCog className="h-4 w-4" /> Save owner
              </Button>
            </div>
          </form>
        )}
      </Modal>

      <Modal
        open={!!suspendTarget}
        onClose={() => !suspensionMutation.isPending && setSuspendTarget(null)}
        title="Suspend VPS"
        maxWidth="lg"
      >
        {suspendTarget && (
          <form onSubmit={confirmSuspension} className="space-y-4">
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
              This immediately applies an administrative lock and requests a graceful Proxmox shutdown. The customer cannot start or restart the VPS until an administrator removes the lock.
            </div>
            <Input
              label="Suspension reason"
              value={suspensionReason}
              onChange={(event) => setSuspensionReason(event.target.value)}
              placeholder="Required"
              required
            />
            {suspensionMutation.error && <ErrorMessage error={suspensionMutation.error} />}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setSuspendTarget(null)}>Cancel</Button>
              <Button type="submit" variant="danger" loading={suspensionMutation.isPending} disabled={!suspensionReason.trim()}>
                <Ban className="h-4 w-4" /> Suspend and shut down
              </Button>
            </div>
          </form>
        )}
      </Modal>

      <Modal
        open={!!unsuspendTarget}
        onClose={() => !unsuspensionMutation.isPending && setUnsuspendTarget(null)}
        title="Unsuspend VPS"
        maxWidth="lg"
      >
        {unsuspendTarget && (
          <div className="space-y-4">
            <p className="text-sm text-slate-600">
              Remove the administrative lock from <span className="font-semibold">{unsuspendTarget.name}</span>? This does not start the VPS automatically.
            </p>
            {unsuspendTarget.admin_suspension_reason && (
              <p className="rounded-md bg-slate-50 p-3 text-sm text-slate-600">
                Suspension reason: {unsuspendTarget.admin_suspension_reason}
              </p>
            )}
            {unsuspensionMutation.error && <ErrorMessage error={unsuspensionMutation.error} />}
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setUnsuspendTarget(null)}>Cancel</Button>
              <Button onClick={() => unsuspensionMutation.mutate(unsuspendTarget.id)} loading={unsuspensionMutation.isPending}>
                <ShieldCheck className="h-4 w-4" /> Unsuspend
              </Button>
            </div>
          </div>
        )}
      </Modal>


      <Modal
        open={!!deleteTarget}
        onClose={() => {
          if (!deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        title="Delete VPS"
        maxWidth="lg"
      >
        {deleteTarget && (
          <form onSubmit={confirmDelete} className="space-y-4">
            <p className="text-sm text-slate-600">
              Terraform destroy will be attempted before metadata is removed. Type{' '}
              <span className="font-semibold text-slate-900">{deleteTarget.name}</span> to continue.
            </p>
            <Input
              label="VPS name confirmation"
              value={deleteConfirmationName}
              onChange={(event) => setDeleteConfirmationName(event.target.value)}
              autoFocus
              autoComplete="off"
            />
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>
                Cancel
              </Button>
              <Button type="submit" variant="danger" loading={deleteMutation.isPending} disabled={deleteConfirmationName !== deleteTarget.name}>
                <Trash2 className="h-4 w-4" />
                Delete VPS
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </Layout>
  );
}

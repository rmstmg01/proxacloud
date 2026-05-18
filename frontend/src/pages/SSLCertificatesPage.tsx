import { useState, type ChangeEvent, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { createSSLCertificate, deleteSSLCertificate, getSSLCertificates } from '../api/sslCertificates';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

export function SSLCertificatesPage() {
  const qc = useQueryClient();
  const { data: certs, isLoading, error } = useQuery({ queryKey: ['ssl-certificates'], queryFn: getSSLCertificates });
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState({
    name: '',
    fullchain_pem: '',
    private_key_pem: '',
  });

  const createMut = useMutation({
    mutationFn: createSSLCertificate,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ssl-certificates'] });
      setCreateOpen(false);
      setForm({ name: '', fullchain_pem: '', private_key_pem: '' });
    },
  });

  const deleteMut = useMutation({
    mutationFn: deleteSSLCertificate,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['ssl-certificates'] });
    },
  });

  const loadPEMFile = (field: 'fullchain_pem' | 'private_key_pem') => (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      setForm((f) => ({ ...f, [field]: String(reader.result ?? '') }));
    };
    reader.readAsText(file);
  };

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    createMut.mutate(form);
  };

  return (
    <Layout>
      <PageHeader
        title="SSL Certificates"
        description="Manage reusable fullchain.pem and privkey.pem pairs for application ingress TLS"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" /> Add SSL Certificate
          </Button>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}

      {certs && (
        <Card>
          {certs.length === 0 ? (
            <CardContent>
              <p className="text-sm text-slate-400">No SSL certificates saved yet.</p>
            </CardContent>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Subject</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">DNS Names</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Expires</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Updated</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {certs.map((cert) => (
                    <tr key={cert.id} className="hover:bg-slate-50">
                      <td className="px-6 py-3 font-medium text-slate-900">{cert.name}</td>
                      <td className="px-6 py-3 text-slate-600">{cert.subject || '-'}</td>
                      <td className="px-6 py-3 text-slate-600 max-w-md truncate">
                        {cert.dns_names?.length ? cert.dns_names.join(', ') : '-'}
                      </td>
                      <td className="px-6 py-3 text-slate-500">
                        {cert.not_after ? new Date(cert.not_after).toLocaleDateString() : '-'}
                      </td>
                      <td className="px-6 py-3 text-slate-500">{new Date(cert.updated_at).toLocaleString()}</td>
                      <td className="px-6 py-3 text-right">
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => {
                            if (window.confirm(`Delete SSL certificate "${cert.name}"?`)) {
                              deleteMut.mutate(cert.id);
                            }
                          }}
                          loading={deleteMut.isPending && deleteMut.variables === cert.id}
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

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="Add SSL Certificate">
        <form onSubmit={handleCreate} className="space-y-4">
          <Input
            label="Name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="proxaops-wildcard"
            required
          />
          <Input label="fullchain.pem" type="file" accept=".pem,.crt" onChange={loadPEMFile('fullchain_pem')} />
          <Textarea
            label="Certificate PEM"
            value={form.fullchain_pem}
            onChange={(e) => setForm({ ...form, fullchain_pem: e.target.value })}
            rows={6}
            className="font-mono text-xs"
            placeholder="-----BEGIN CERTIFICATE-----"
            required
          />
          <Input label="privkey.pem" type="file" accept=".pem,.key" onChange={loadPEMFile('private_key_pem')} />
          <Textarea
            label="Private key PEM"
            value={form.private_key_pem}
            onChange={(e) => setForm({ ...form, private_key_pem: e.target.value })}
            rows={6}
            className="font-mono text-xs"
            placeholder="-----BEGIN PRIVATE KEY-----"
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

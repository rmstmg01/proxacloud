import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import RFB from '@novnc/novnc';
import { ArrowLeft, Monitor, Power, RefreshCw } from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import { openVPSConsole } from '../api/vps';
import { Button } from '../components/ui/Button';

type ConsoleState = 'connecting' | 'connected' | 'disconnected' | 'error';

export function VPSConsolePage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { token } = useAuth();
  const vpsID = Number(id);
  const screenRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [state, setState] = useState<ConsoleState>('connecting');
  const [message, setMessage] = useState('Opening console...');
  const [connectKey, setConnectKey] = useState(0);

  useEffect(() => {
    if (!Number.isFinite(vpsID) || vpsID <= 0) {
      navigate('/vps');
      return undefined;
    }
    if (!token || !screenRef.current) {
      setState('error');
      setMessage('Login session is missing. Please sign in again.');
      return undefined;
    }

    let cancelled = false;
    let rfb: RFB | null = null;
    let consolePassword = '';
    setState('connecting');
    setMessage('Creating console session...');

    const handleConnect = () => {
      setState('connected');
      setMessage('Connected');
    };
    const handleDisconnect = (event: Event) => {
      const detail = (event as CustomEvent<{ clean?: boolean }>).detail;
      setState(detail?.clean ? 'disconnected' : 'error');
      setMessage(detail?.clean ? 'Console disconnected.' : 'Console connection closed unexpectedly.');
    };
    const handleSecurityFailure = (event: Event) => {
      const detail = (event as CustomEvent<{ reason?: string }>).detail;
      setState('error');
      setMessage(detail?.reason || 'Console security negotiation failed.');
    };
    const handleCredentialsRequired = () => {
      if (rfb) {
        rfb.sendCredentials({ password: consolePassword });
      }
    };

    openVPSConsole(vpsID)
      .then((session) => {
        if (cancelled || !screenRef.current) return;
        consolePassword = session.password;
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const separator = session.websocket_path.includes('?') ? '&' : '?';
        const wsURL = `${wsProtocol}//${window.location.host}${session.websocket_path}${separator}token=${encodeURIComponent(token)}`;
        rfb = new RFB(screenRef.current, wsURL, {
          shared: true,
          wsProtocols: ['binary'],
          credentials: { password: session.password },
        });
        rfb.scaleViewport = true;
        rfb.resizeSession = false;
        rfb.viewOnly = false;
        rfbRef.current = rfb;
        rfb.addEventListener('connect', handleConnect);
        rfb.addEventListener('disconnect', handleDisconnect);
        rfb.addEventListener('securityfailure', handleSecurityFailure);
        rfb.addEventListener('credentialsrequired', handleCredentialsRequired);
        setMessage('Connecting to VM console...');
      })
      .catch((err) => {
        if (cancelled) return;
        const msg = err?.response?.data?.error || err?.message || 'Unable to create console session.';
        setState('error');
        setMessage(msg);
      });

    return () => {
      cancelled = true;
      rfb?.removeEventListener('connect', handleConnect);
      rfb?.removeEventListener('disconnect', handleDisconnect);
      rfb?.removeEventListener('securityfailure', handleSecurityFailure);
      rfb?.removeEventListener('credentialsrequired', handleCredentialsRequired);
      rfb?.disconnect();
      if (rfb && rfbRef.current === rfb) {
        rfbRef.current = null;
      }
    };
  }, [connectKey, navigate, token, vpsID]);

  const reconnect = () => {
    rfbRef.current?.disconnect();
    setConnectKey((current) => current + 1);
  };

  return (
    <div className="flex min-h-screen flex-col bg-slate-950 text-white">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800 bg-slate-900 px-4 py-3">
        <div className="flex items-center gap-3">
          <Link to={`/vps/${vpsID}`} className="inline-flex items-center gap-1 text-sm text-slate-300 hover:text-white">
            <ArrowLeft className="h-4 w-4" />
            Back
          </Link>
          <div className="h-5 w-px bg-slate-700" />
          <div className="flex items-center gap-2">
            <Monitor className="h-4 w-4 text-indigo-300" />
            <div>
              <h1 className="text-sm font-semibold">VPS Console</h1>
              <p className="text-xs text-slate-400">VM #{Number.isFinite(vpsID) ? vpsID : ''}</p>
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className={`rounded-full px-2.5 py-1 text-xs font-medium ${state === 'connected' ? 'bg-emerald-500/15 text-emerald-200' : state === 'error' ? 'bg-red-500/15 text-red-200' : 'bg-slate-700 text-slate-200'}`}>
            {message}
          </span>
          <Button variant="secondary" size="sm" onClick={() => rfbRef.current?.sendCtrlAltDel()} disabled={state !== 'connected'}>
            <Power className="h-4 w-4" />
            Ctrl Alt Del
          </Button>
          <Button variant="secondary" size="sm" onClick={reconnect}>
            <RefreshCw className="h-4 w-4" />
            Reconnect
          </Button>
        </div>
      </header>
      <main className="min-h-0 flex-1 bg-black">
        <div ref={screenRef} className="h-[calc(100vh-64px)] w-full overflow-hidden bg-black" />
      </main>
    </div>
  );
}

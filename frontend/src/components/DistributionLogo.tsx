import { siAlmalinux, siDebian, siFedora, siRockylinux, siUbuntu, type SimpleIcon } from 'simple-icons';
import { Server } from 'lucide-react';

const icons: Record<string, SimpleIcon> = {
  almalinux: siAlmalinux,
  debian: siDebian,
  fedora: siFedora,
  rocky: siRockylinux,
  rockylinux: siRockylinux,
  ubuntu: siUbuntu,
};

export function DistributionLogo({ distribution, className = 'h-6 w-6' }: { distribution: string; className?: string }) {
  const icon = icons[distribution.toLowerCase()];
  if (!icon) return <Server className={className} aria-hidden="true" />;

  return (
    <svg className={className} role="img" aria-label={`${icon.title} logo`} viewBox="0 0 24 24" fill={`#${icon.hex}`}>
      <path d={icon.path} />
    </svg>
  );
}

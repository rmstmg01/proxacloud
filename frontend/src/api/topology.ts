import client from './client';
import type { PlanResponse, TopologyType } from '../types';

export const planTopology = (body: {
  topology_type: TopologyType;
  control_plane_count: number;
  worker_count: number;
}) => client.post<PlanResponse>('/topology/plan', body).then((r) => r.data);

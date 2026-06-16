import client from './client';
import type { StorageInventory } from '../types';

export const getStorageInventory = () => client.get<StorageInventory>('/storage').then((r) => r.data);

import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';

/** Endereço de um CEP (consulta do backend ao ViaCEP). `ibgeCode` é o mesmo código do catálogo de municípios. */
export interface ZipAddress {
  zip: string;
  street: string;
  district: string;
  city: string;
  uf: string;
  ibgeCode: string;
}

export async function lookupZip(zip: string): Promise<ZipAddress> {
  const digits = zip.replace(/\D/g, '');
  return (await apiClient<ApiResponse<ZipAddress>>(`/admin/catalog/zip-codes/${digits}`)).data;
}

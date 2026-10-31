'use client';

import { useEffect, useRef, useState } from 'react';
import { ApiError } from '@/lib/api/errors';
import { lookupZip, type ZipAddress } from './api/service';

/**
 * Consulta o endereço de um CEP quando o campo perde o foco. `apply` recebe o endereço encontrado; se a consulta falhar,
 * `message` explica e a pessoa preenche à mão. Não repete a consulta do mesmo CEP.
 */
export function useZipLookup(apply: (address: ZipAddress) => void) {
  const [state, setState] = useState({ loading: false, message: '' });
  const last = useRef('');
  const applyRef = useRef(apply);
  useEffect(() => {
    applyRef.current = apply;
  });

  const lookup = async (raw: string) => {
    const digits = raw.replace(/\D/g, '');
    if (digits.length !== 8 || digits === last.current) return;
    last.current = digits;
    setState({ loading: true, message: '' });
    try {
      applyRef.current(await lookupZip(digits));
      setState({ loading: false, message: '' });
    } catch (e) {
      last.current = '';
      setState({
        loading: false,
        message:
          e instanceof ApiError && e.code === 'cep_not_found'
            ? 'CEP não encontrado. Preencha o endereço.'
            : 'Não foi possível consultar o CEP. Preencha o endereço.'
      });
    }
  };
  return { ...state, lookup };
}

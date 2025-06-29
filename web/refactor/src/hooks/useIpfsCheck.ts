import { useMutation } from '@tanstack/react-query';
import type { FormData, CheckResponse, Provider } from '../types';

const checkIpfs = async (formData: FormData): Promise<Provider[]> => {
  const params = new URLSearchParams();
  params.append('cid', formData.cid);
  if (formData.multiaddr) params.append('multiaddr', formData.multiaddr);
  params.append('timeoutSeconds', formData.timeoutSeconds.toString());

  const response = await fetch(`${formData.backendURL}/check?${params}`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(`Backend error: ${response.status} ${await response.text()}`);
  }

  const data = await response.json();

  // If the response is already an array of providers, return it
  if (Array.isArray(data)) {
    return data;
  }

  // If it's a CheckResponse, convert it to a Provider array
  const checkResponse = data as CheckResponse;
  const provider: Provider = {
    ID: 'local',
    Source: 'DHT',
    ConnectionError: checkResponse.ConnectionError,
    ConnectionMaddrs: checkResponse.ConnectionMaddrs,
    DataAvailableOverBitswap: checkResponse.DataAvailableOverBitswap,
    DataAvailableOverHTTP: checkResponse.DataAvailableOverHTTP,
  };

  return [provider];
};

export const useIpfsCheck = () => {
  return useMutation({
    mutationFn: checkIpfs,
  });
}; 

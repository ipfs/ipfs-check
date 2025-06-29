import { CheckIcon, CrossIcon } from './StatusIcons';
import { ProviderCard } from './ProviderCard';
import type { Provider } from '../types';

interface ProviderListProps {
  providers: Provider[];
}

export const ProviderList = ({ providers }: ProviderListProps) => {
  if (providers.length === 0) {
    return (
      <div className="bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center">
        <CrossIcon />
        <span>No providers found for the given CID</span>
      </div>
    );
  }

  const successfulProviders = providers.reduce((acc, provider) => {
    if (
      provider.ConnectionError === '' &&
      (provider.DataAvailableOverBitswap?.Found === true || provider.DataAvailableOverHTTP?.Found === true)
    ) {
      acc++;
    }
    return acc;
  }, 0);

  // Sort providers
  const sortedProviders = [...providers].sort((a, b) => {
    const aHasData = a.DataAvailableOverBitswap?.Found || a.DataAvailableOverHTTP?.Found;
    const bHasData = b.DataAvailableOverBitswap?.Found || b.DataAvailableOverHTTP?.Found;

    // First order by data availability
    if (aHasData && !bHasData) return -1;
    if (!aHasData && bHasData) return 1;

    const aHasBitswap = a.DataAvailableOverBitswap?.Enabled;
    const bHasBitswap = b.DataAvailableOverBitswap?.Enabled;

    // Then order HTTP first
    if (aHasBitswap && !bHasBitswap) return 1;
    if (!aHasBitswap && bHasBitswap) return -1;

    const aSource = a.Source;
    const bSource = b.Source;

    // Then order Amino DHT first
    if (aSource === 'IPNI' && bSource !== 'IPNI') return 1;
    if (aSource !== 'IPNI' && bSource === 'IPNI') return -1;

    // Then order by connection errors
    if (a.ConnectionError === '' && b.ConnectionError !== '') {
      return -1;
    } else if (a.ConnectionError !== '' && b.ConnectionError === '') {
      return 1;
    }

    // Finally, show providers with addresses first
    const aHasAddrs = a.Addrs && a.Addrs.length > 0;
    const bHasAddrs = b.Addrs && b.Addrs.length > 0;
    if (aHasAddrs && !bHasAddrs) {
      return -1;
    } else if (!aHasAddrs && bHasAddrs) {
      return 1;
    }

    return 0;
  });

  return (
    <div>
      <div className="mb-4">
        <span className="text-lg font-bold">
          {successfulProviders > 0 ? <CheckIcon /> : <CrossIcon />} Found {successfulProviders} working providers
        </span>{' '}
        <span className="text-gray-600">
          (out of {providers.length} provider records sampled from Amino DHT and IPNI) that could be connected to and had
          the CID available over Bitswap:
        </span>
      </div>
      <div className="grid gap-4 grid-cols-1">
        {sortedProviders.map((provider) => (
          <ProviderCard key={provider.ID} provider={provider} />
        ))}
      </div>
    </div>
  );
}; 

import { CheckIcon, CrossIcon } from './StatusIcons';
import type { Provider } from '../types';

interface ProviderCardProps {
  provider: Provider;
}

export const ProviderCard = ({ provider }: ProviderCardProps) => {
  const couldConnect = provider.ConnectionError === '';
  const hasBitswap = provider.DataAvailableOverBitswap?.Enabled === true;
  const hasHTTP = provider.DataAvailableOverHTTP?.Enabled === true;
  const foundBitswap = provider.DataAvailableOverBitswap?.Found;
  const foundHTTP = provider.DataAvailableOverHTTP?.Found;
  const isUnsuccessful = !couldConnect || (!foundBitswap && !foundHTTP);
  const cardBg = isUnsuccessful ? 'bg-red-50 border-red-200' : 'bg-white border-gray-200';

  const renderMultiaddrs = (addrs: string[]) => (
    <div className="text-xs text-gray-600 mt-2">
      <span className="font-bold">Peer Multiaddrs:</span>
      <br />
      <span className="font-mono block ml-4 break-all whitespace-pre-wrap">
        {addrs.join('\n')}
      </span>
    </div>
  );

  return (
    <div className={`rounded-lg shadow ${cardBg} p-4 border`}>
      <div className="flex justify-between items-center mb-2">
        <span className="font-mono text-xs bg-gray-100 px-2 py-1 rounded mr-2 break-all">
          {provider.ID}
        </span>
        <div className="flex gap-2">
          {hasBitswap && (
            <span className="px-2 py-1 rounded bg-green-100 text-green-700 text-xs font-bold">
              Bitswap
            </span>
          )}
          {hasHTTP && (
            <span className="px-2 py-1 rounded bg-blue-100 text-blue-700 text-xs font-bold">
              HTTP
            </span>
          )}
          {provider.Source && (
            <span
              className={`px-2 py-1 rounded text-xs font-bold ${
                provider.Source === 'IPNI'
                  ? 'bg-orange-100 text-orange-700'
                  : 'bg-purple-100 text-purple-700'
              }`}
            >
              {provider.Source}
            </span>
          )}
        </div>
      </div>

      {hasBitswap && (
        <>
          <div className="flex items-center text-sm mb-1">
            {couldConnect ? <CheckIcon /> : <CrossIcon />}
            <span>
              Libp2p connected:{' '}
              <span className="font-mono">
                {couldConnect ? 'Yes' : provider.ConnectionError}
              </span>
            </span>
          </div>
          {couldConnect && provider.DataAvailableOverBitswap && (
            <div className="flex items-center text-sm mb-1 ml-6">
              {provider.DataAvailableOverBitswap.Found ? <CheckIcon /> : <CrossIcon />}
              <span>
                Bitswap Check:{' '}
                <span className="font-mono">
                  {provider.DataAvailableOverBitswap.Found ? 'Found' : 'Not found'}{' '}
                  {provider.DataAvailableOverBitswap.Error || ''}
                </span>
              </span>
            </div>
          )}
        </>
      )}

      {hasHTTP && provider.DataAvailableOverHTTP && (
        <>
          <div className="flex items-center text-sm mb-1">
            {provider.DataAvailableOverHTTP.Connected ? <CheckIcon /> : <CrossIcon />}
            <span>
              HTTP Connected:{' '}
              <span className="font-mono">
                {provider.DataAvailableOverHTTP.Connected ? 'Yes' : 'No'}
              </span>
            </span>
          </div>
          <div className="flex items-center text-sm mb-1 ml-6">
            {provider.DataAvailableOverHTTP.Requested ? <CheckIcon /> : <CrossIcon />}
            <span>
              HTTP HEAD request:{' '}
              <span className="font-mono">
                {provider.DataAvailableOverHTTP.Requested ? 'Yes' : 'No'}
              </span>
            </span>
          </div>
          <div className="flex items-center text-sm mb-1 ml-6">
            {provider.DataAvailableOverHTTP.Found ? <CheckIcon /> : <CrossIcon />}
            <span>
              HTTP Found:{' '}
              <span className="font-mono">
                {provider.DataAvailableOverHTTP.Found ? 'Yes' : 'No'} {provider.DataAvailableOverHTTP.Error ?? ''}
              </span>
            </span>
          </div>
        </>
      )}

      {couldConnect && provider.ConnectionMaddrs && provider.ConnectionMaddrs.length > 0 && (
        renderMultiaddrs(provider.ConnectionMaddrs)
      )}

      {provider.Addrs && provider.Addrs.length > 0 && (
        renderMultiaddrs(provider.Addrs)
      )}
    </div>
  );
}; 

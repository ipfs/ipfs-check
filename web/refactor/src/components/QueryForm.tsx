import { useState } from 'react';
import { z } from 'zod';
import { useIpfsCheck } from '../hooks/useIpfsCheck';
import type { FormData } from '../types';
import { ProviderList } from './ProviderList';

const formSchema = z.object({
  cid: z.string().min(1, 'CID is required'),
  multiaddr: z.string().optional(),
  backendURL: z.string().url('Must be a valid URL'),
  timeoutSeconds: z.number().min(1).max(60),
});

export const QueryForm = () => {
  const [formData, setFormData] = useState<FormData>({
    cid: '',
    multiaddr: '',
    backendURL: 'http://localhost:8080',
    timeoutSeconds: 30,
  });
  const [validationErrors, setValidationErrors] = useState<Record<string, string>>({});

  const { mutate, isPending, error, data } = useIpfsCheck();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    try {
      formSchema.parse(formData);
      setValidationErrors({});
      mutate(formData);
    } catch (err) {
      if (err instanceof z.ZodError) {
        const errors: Record<string, string> = {};
        err.errors.forEach((error) => {
          if (error.path[0]) {
            errors[error.path[0].toString()] = error.message;
          }
        });
        setValidationErrors(errors);
      }
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: name === 'timeoutSeconds' ? parseInt(value, 10) : value,
    }));
    // Clear validation error when user starts typing
    if (validationErrors[name]) {
      setValidationErrors(prev => ({ ...prev, [name]: '' }));
    }
  };

  const getInputClassName = (hasError: boolean) => {
    return `mt-1 block w-full rounded-md shadow-sm focus:ring-indigo-500 focus:border-indigo-500 sm:text-sm ${
      hasError ? 'border-red-300' : 'border-gray-300'
    }`;
  };

  return (
    <div>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <label htmlFor="cid" className="block text-sm font-medium text-gray-700">
            CID
          </label>
          <input
            type="text"
            id="cid"
            name="cid"
            value={formData.cid}
            onChange={handleChange}
            className={getInputClassName(!!validationErrors.cid)}
            placeholder="Enter CID"
            aria-invalid={!!validationErrors.cid}
            aria-describedby={validationErrors.cid ? 'cid-error' : undefined}
          />
          {validationErrors.cid && (
            <p className="mt-1 text-sm text-red-600" id="cid-error">
              {validationErrors.cid}
            </p>
          )}
        </div>

        <div>
          <label htmlFor="multiaddr" className="block text-sm font-medium text-gray-700">
            Multiaddr (optional)
          </label>
          <input
            type="text"
            id="multiaddr"
            name="multiaddr"
            value={formData.multiaddr}
            onChange={handleChange}
            className={getInputClassName(!!validationErrors.multiaddr)}
            placeholder="Enter multiaddr"
            aria-invalid={!!validationErrors.multiaddr}
            aria-describedby={validationErrors.multiaddr ? 'multiaddr-error' : undefined}
          />
          {validationErrors.multiaddr && (
            <p className="mt-1 text-sm text-red-600" id="multiaddr-error">
              {validationErrors.multiaddr}
            </p>
          )}
        </div>

        <div>
          <label htmlFor="backendURL" className="block text-sm font-medium text-gray-700">
            Backend URL
          </label>
          <input
            type="url"
            id="backendURL"
            name="backendURL"
            value={formData.backendURL}
            onChange={handleChange}
            className={getInputClassName(!!validationErrors.backendURL)}
            aria-invalid={!!validationErrors.backendURL}
            aria-describedby={validationErrors.backendURL ? 'backendURL-error' : undefined}
          />
          {validationErrors.backendURL && (
            <p className="mt-1 text-sm text-red-600" id="backendURL-error">
              {validationErrors.backendURL}
            </p>
          )}
        </div>

        <div>
          <label htmlFor="timeoutSeconds" className="block text-sm font-medium text-gray-700">
            Timeout (seconds)
          </label>
          <input
            type="range"
            id="timeoutSeconds"
            name="timeoutSeconds"
            min="1"
            max="60"
            value={formData.timeoutSeconds}
            onChange={handleChange}
            className="mt-1 block w-full"
            aria-valuemin={1}
            aria-valuemax={60}
            aria-valuenow={formData.timeoutSeconds}
            aria-valuetext={`${formData.timeoutSeconds} seconds`}
          />
          <span className="text-sm text-gray-500" aria-live="polite">
            {formData.timeoutSeconds} seconds
          </span>
        </div>

        <button
          type="submit"
          disabled={isPending}
          className="inline-flex justify-center rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {isPending ? 'Checking...' : 'Check IPFS'}
        </button>

        {error && (
          <div className="rounded-md bg-red-50 p-4" role="alert">
            <div className="flex">
              <div className="ml-3">
                <h3 className="text-sm font-medium text-red-800">Error</h3>
                <div className="mt-2 text-sm text-red-700">
                  <p>{error.message}</p>
                </div>
              </div>
            </div>
          </div>
        )}
      </form>

      {isPending && (
        <div className="mt-8 text-center py-8">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-indigo-600 mx-auto"></div>
          <p className="mt-4 text-gray-600">Checking IPFS providers...</p>
        </div>
      )}

      {!isPending && data && data.length > 0 && (
        <div className="mt-8">
          <ProviderList providers={data} />
        </div>
      )}

      {!isPending && data && data.length === 0 && (
        <div className="mt-8 text-center py-8">
          <p className="text-gray-600">No providers found for the given CID.</p>
        </div>
      )}
    </div>
  );
}; 

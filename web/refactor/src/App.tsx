import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { QueryForm } from './components/QueryForm';
import { ProviderList } from './components/ProviderList';

const queryClient = new QueryClient();

function AppContent() {
  return (
    <div className="min-h-screen bg-gray-50 py-8">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="max-w-3xl mx-auto">
          <h1 className="text-3xl font-bold text-gray-900 mb-8">IPFS Check</h1>
          <div className="bg-white shadow sm:rounded-lg p-6 mb-8">
            <QueryForm />
          </div>
        </div>
      </div>
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppContent />
    </QueryClientProvider>
  );
}

export interface Provider {
  ID: string;
  Source?: 'IPNI' | 'DHT';
  ConnectionError: string;
  ConnectionMaddrs?: string[];
  Addrs?: string[];
  DataAvailableOverBitswap?: {
    Enabled: boolean;
    Found?: boolean;
    Error?: string;
    Responded?: boolean;
  };
  DataAvailableOverHTTP?: {
    Enabled: boolean;
    Connected?: boolean;
    Requested?: boolean;
    Found?: boolean;
    Error?: string;
  };
}

export interface CheckResponse {
  ConnectionError: string;
  ConnectionMaddrs: string[];
  PeerFoundInDHT: Record<string, number>;
  ProviderRecordFromPeerInDHT: boolean;
  ProviderRecordFromPeerInIPNI: boolean;
  DataAvailableOverBitswap?: {
    Enabled: boolean;
    Found?: boolean;
    Error?: string;
    Responded?: boolean;
  };
  DataAvailableOverHTTP?: {
    Enabled: boolean;
    Connected?: boolean;
    Requested?: boolean;
    Found?: boolean;
    Error?: string;
  };
}

export interface FormData {
  cid: string;
  multiaddr: string;
  backendURL: string;
  timeoutSeconds: number;
} 

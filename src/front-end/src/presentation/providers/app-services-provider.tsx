import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { container } from '@/application/di';
import { ensureReactDashboardBootstrap } from '@/presentation/bootstrap-react-dashboard';

type AppServicesContextValue = {
  container: typeof container;
  ready: boolean;
};

const AppServicesContext = createContext<AppServicesContextValue | null>(null);

export function useAppServices(): AppServicesContextValue {
  const ctx = useContext(AppServicesContext);
  if (!ctx) {
    throw new Error('useAppServices must be used within AppServicesProvider');
  }
  return ctx;
}

export function AppServicesProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void ensureReactDashboardBootstrap()
      .then(() => {
        if (!cancelled) {
          setReady(true);
        }
      })
      .catch((error) => {
        container.getLogger().error('React dashboard bootstrap failed, continuing with degraded mode', {
          'error.message': error instanceof Error ? error.message : String(error),
        });
        if (!cancelled) {
          setReady(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const value = useMemo(() => ({ container, ready }), [ready]);

  return (
    <AppServicesContext.Provider value={value}>{children}</AppServicesContext.Provider>
  );
}

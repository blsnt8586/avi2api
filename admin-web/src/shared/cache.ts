import React, { useState } from "react";

const playgroundCachePrefix = "leo-playground:v2:";

const playgroundResultDB = "leo-playground-results-v1";

const playgroundResultStore = "results";

export function useSessionState<T>(key: string, initial: T) {
  const storageKey = playgroundCachePrefix + key;
  const [value, setValue] = useState<T>(() => {
    try {
      const cached = sessionStorage.getItem(storageKey);
      return cached === null ? initial : (JSON.parse(cached) as T);
    } catch {
      return initial;
    }
  });
  React.useEffect(() => {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(value));
    } catch {}
  }, [storageKey, value]);
  return [value, setValue] as const;
}

export function useDebouncedValue<T>(value: T, delay = 250) {
  const [debounced, setDebounced] = useState(value);
  React.useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [delay, value]);
  return debounced;
}

export function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() =>
    typeof window !== "undefined" ? window.matchMedia(query).matches : false,
  );
  React.useEffect(() => {
    const media = window.matchMedia(query);
    const update = () => setMatches(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [query]);
  return matches;
}

function openPlaygroundResultDB() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(playgroundResultDB, 1);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(playgroundResultStore))
        request.result.createObjectStore(playgroundResultStore);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function readCachedResult<T>(key: string) {
  const db = await openPlaygroundResultDB();
  return new Promise<T | undefined>((resolve, reject) => {
    const transaction = db.transaction(playgroundResultStore, "readonly");
    const request = transaction.objectStore(playgroundResultStore).get(key);
    request.onsuccess = () => resolve(request.result as T | undefined);
    request.onerror = () => reject(request.error);
    transaction.oncomplete = () => db.close();
    transaction.onabort = () => db.close();
  });
}

async function writeCachedResult<T>(key: string, value: T) {
  const db = await openPlaygroundResultDB();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(playgroundResultStore, "readwrite");
    transaction.objectStore(playgroundResultStore).put(value, key);
    transaction.oncomplete = () => {
      db.close();
      resolve();
    };
    transaction.onerror = () => {
      db.close();
      reject(transaction.error);
    };
    transaction.onabort = () => {
      db.close();
      reject(transaction.error);
    };
  });
}

async function deleteCachedResult(key: string) {
  const db = await openPlaygroundResultDB();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(playgroundResultStore, "readwrite");
    transaction.objectStore(playgroundResultStore).delete(key);
    transaction.oncomplete = () => {
      db.close();
      resolve();
    };
    transaction.onerror = () => {
      db.close();
      reject(transaction.error);
    };
    transaction.onabort = () => {
      db.close();
      reject(transaction.error);
    };
  });
}

async function clearCachedResults() {
  const db = await openPlaygroundResultDB();
  return new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(playgroundResultStore, "readwrite");
    transaction.objectStore(playgroundResultStore).clear();
    transaction.oncomplete = () => {
      db.close();
      resolve();
    };
    transaction.onerror = () => {
      db.close();
      reject(transaction.error);
    };
    transaction.onabort = () => {
      db.close();
      reject(transaction.error);
    };
  });
}

export function useCachedResult<T>(key: string) {
  const [value, setValue] = useState<T | null>(null);
  const [ready, setReady] = useState(false);
  React.useEffect(() => {
    let active = true;
    readCachedResult<T>(key)
      .then((result) => {
        if (active) setValue(result || null);
      })
      .catch(() => {})
      .finally(() => {
        if (active) setReady(true);
      });
    return () => {
      active = false;
    };
  }, [key]);
  const update = React.useCallback(
    (next: T | null) => {
      setValue(next);
      void (
        next === null ? deleteCachedResult(key) : writeCachedResult(key, next)
      ).catch(() => {});
    },
    [key],
  );
  return [value, update, ready] as const;
}

export async function clearPlaygroundCache() {
  Object.keys(sessionStorage)
    .filter((key) => key.startsWith(playgroundCachePrefix))
    .forEach((key) => sessionStorage.removeItem(key));
  await clearCachedResults().catch(() => {});
  window.location.reload();
}

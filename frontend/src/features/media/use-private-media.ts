import { useEffect, useState } from "react";
import { apiBlob } from "@/shared/api/client";

export function usePrivateMedia(kind: "images" | "videos", id: string) {
  const [result, setResult] = useState<{ request: string; url?: string; error: boolean }>();
  const [revision, setRevision] = useState(0);
  const path = `/api/admin/v1/media/${kind}/${encodeURIComponent(id)}/content`;
  const request = `${path}:${revision}`;
  useEffect(() => {
    const controller = new AbortController();
    let objectURL: string | undefined;
    void apiBlob(path, controller.signal).then(blob => {
      if (controller.signal.aborted) return;
      objectURL = URL.createObjectURL(blob);
      setResult({ request, url: objectURL, error: false });
    }).catch(() => {
      if (!controller.signal.aborted) setResult({ request, error: true });
    });
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [path, request]);
  const current = result?.request === request ? result : undefined;
  return { url: current?.url, error: current?.error ?? false, retry: () => setRevision(r => r + 1) };
}

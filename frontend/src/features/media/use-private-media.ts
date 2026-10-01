import { useEffect, useState } from "react";
import { apiBlob } from "@/shared/api/client";

export function usePrivateMedia(kind: "images" | "videos", id: string) {
  const [url,setURL] = useState<string>();
  const [error,setError] = useState(false);
  const [revision,setRevision] = useState(0);
  useEffect(()=>{
    const controller = new AbortController(); let objectURL: string | undefined;
    setURL(undefined); setError(false);
    void apiBlob(`/api/admin/v1/media/${kind}/${encodeURIComponent(id)}/content`, controller.signal).then(blob=>{
      if(controller.signal.aborted)return;
      objectURL=URL.createObjectURL(blob);setURL(objectURL);
    }).catch(()=>{if(!controller.signal.aborted)setError(true)});
    return ()=>{controller.abort();if(objectURL)URL.revokeObjectURL(objectURL)};
  },[kind,id,revision]);
  return {url,error,retry:()=>setRevision(r=>r+1)};
}

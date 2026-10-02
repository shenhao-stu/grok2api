import type { MediaAssetDTO, ImageStatsDTO, MediaJobDTO, VideoStatsDTO } from "@/features/media/types";
import { apiRequest, type PaginatedDTO } from "@/shared/api/client";
import {
  createObjectDecoder,
  createPaginatedDecoder,
  createValidatedDecoder,
  decodeCountResult,
  hasShape,
  isNumber,
  isString,
  isOneOf,
} from "@/shared/api/decoder";
import type { SortOrder } from "@/shared/lib/table-sort";

export type ListImagesInput = {
  page: number;
  pageSize: number;
  search?: string;
};

export type ListVideosInput = {
  page: number;
  pageSize: number;
  status?: MediaJobDTO["status"] | "";
  search?: string;
  sortBy?: string;
  sortOrder?: SortOrder;
};

const mediaAssetShape = {
  id: isString,
  kind: isString,
  mimeType: isString,
  sizeBytes: isNumber,
  sha256: isString,
  createdAt: isString,
  url: isString,
};

const mediaJobShape = {
  id: isString,
  model: isString,
  prompt: isString,
  status: isOneOf("queued", "in_progress", "completed", "failed"),
  progress: isNumber,
  seconds: isNumber,
  size: isString,
  quality: isString,
  accountName: isString,
  clientKeyName: isString,
  createdAt: isString,
  completedAt: (value: unknown) => value === null || isString(value),
  errorMessage: isString,
  assetId: isString,
};

const decodeImageStats = createObjectDecoder<ImageStatsDTO>("image stats", {
  totalImages: isNumber,
  totalBytes: isNumber,
});
const decodeVideoStats = createObjectDecoder<VideoStatsDTO>("video stats", {
  totalJobs: isNumber,
  completed: isNumber,
  failed: isNumber,
  inProgress: isNumber,
  queued: isNumber,
});

export function listImages(input: ListImagesInput): Promise<PaginatedDTO<MediaAssetDTO>> {
  const query = new URLSearchParams({ page: String(input.page), pageSize: String(input.pageSize) });
  if (input.search) query.set("search", input.search);
  return apiRequest(`/api/admin/v1/media/images?${query}`, {}, createPaginatedDecoder(hasShape(mediaAssetShape)));
}

export function getImageStats(): Promise<ImageStatsDTO> {
  return apiRequest("/api/admin/v1/media/images/stats", {}, decodeImageStats);
}

export function deleteImages(ids: string[]): Promise<{ deleted: number }> {
  return apiRequest("/api/admin/v1/media/images", { method: "DELETE", body: { ids } }, decodeCountResult<{ deleted: number }>("deleted"));
}

export function listVideos(input: ListVideosInput): Promise<PaginatedDTO<MediaJobDTO>> {
  const query = new URLSearchParams({ page: String(input.page), pageSize: String(input.pageSize) });
  if (input.status) query.set("status", input.status);
  if (input.search) query.set("search", input.search);
  if (input.sortBy && input.sortOrder) {
    query.set("sortBy", input.sortBy);
    query.set("sortOrder", input.sortOrder);
  }
  return apiRequest(`/api/admin/v1/media/videos?${query}`, {}, createPaginatedDecoder(hasShape(mediaJobShape)));
}

export function getVideoStats(): Promise<VideoStatsDTO> {
  return apiRequest("/api/admin/v1/media/videos/stats", {}, decodeVideoStats);
}

export function deleteVideos(ids: string[]): Promise<{ deleted: number }> {
  return apiRequest("/api/admin/v1/media/videos", { method: "DELETE", body: { ids } }, decodeCountResult<{ deleted: number }>("deleted"));
}

// 临时输入不会进入图库，也不会生成公开 URL；任务只持久化短 file_id。
export type MediaInputDTO = {
  fileId: string;
  kind: string;
  mimeType: string;
  sizeBytes: number;
  expiresAt: string;
};

const decodeMediaInput = createValidatedDecoder<MediaInputDTO>("media input", hasShape({
  fileId: isString,
  kind: isString,
  mimeType: isString,
  sizeBytes: isNumber,
  expiresAt: isString,
}));

export function importVideoInputFromURL(url: string): Promise<MediaInputDTO> {
  return apiRequest("/api/admin/v1/media/inputs/import", { method: "POST", body: { url } }, decodeMediaInput);
}

type InputUploadDTO = {uploadId:string;offset:number;chunkBytes:number;expiresAt:string};
const decodeInputUpload = createValidatedDecoder<InputUploadDTO>("input upload",hasShape({
  uploadId:isString,offset:isNumber,chunkBytes:isNumber,expiresAt:isString,
}));

// Each body remains below CDN limits, including an exactly 100 MiB source file.
export async function uploadMediaInput(file: File): Promise<MediaInputDTO> {
  if(file.size<1||file.size>100*1024*1024) throw new Error("File must be between 1 byte and 100 MiB");
  const path="/api/admin/v1/media/inputs/uploads";
  const upload=await apiRequest(path,{method:"POST",body:{sizeBytes:file.size,mimeType:file.type}},decodeInputUpload);
  if(!/^[0-9a-f]{48}$/.test(upload.uploadId)||upload.chunkBytes<1||upload.chunkBytes>8*1024*1024) throw new Error("Invalid upload session");
  try {
    for(let offset=0;offset<file.size;){
      const chunk=file.slice(offset,offset+upload.chunkBytes);
      const result=await apiRequest(`${path}/${upload.uploadId}?offset=${offset}`,{method:"PUT",headers:{"Content-Type":"application/octet-stream"},body:chunk},decodeInputUpload);
      if(result.offset!==offset+chunk.size) throw new Error("Upload offset mismatch");
      offset=result.offset;
    }
    return await apiRequest(`${path}/${upload.uploadId}/complete`,{method:"POST"},decodeMediaInput);
  } catch(error) {
    await apiRequest(`${path}/${upload.uploadId}`,{method:"DELETE"},value=>value).catch(()=>undefined);
    throw error;
  }
}

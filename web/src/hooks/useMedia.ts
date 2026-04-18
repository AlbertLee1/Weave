import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  uploadMedia,
  deleteMedia,
  type MediaAsset,
  type UploadProgress,
} from '../api/media';

export interface UploadMediaVariables {
  file: File;
  realm?: string;
  onProgress?: (progress: UploadProgress) => void;
}

/** Mutation hook around POST /api/v2/media. Invalidates `objects` so any
 *  list that depends on media-typed properties refetches. */
export function useUploadMedia() {
  const queryClient = useQueryClient();
  return useMutation<MediaAsset, Error, UploadMediaVariables>({
    mutationFn: (vars) =>
      uploadMedia(vars.file, {
        realm: vars.realm,
        onProgress: vars.onProgress,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
    },
  });
}

/** Mutation hook around DELETE /api/v2/media/{rid}. */
export function useDeleteMedia() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (rid) => deleteMedia(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
    },
  });
}

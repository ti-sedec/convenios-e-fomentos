import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';
import type { ReviewerActivityItem, ReviewerMySummary, ReviewerOverview } from './types';

export async function getReviewerOverview() {
  return (await apiClient<ApiResponse<ReviewerOverview>>('/admin/projects/department/overview'))
    .data;
}

export async function getReviewerActivity(limit?: number) {
  return (
    await apiClient<ApiResponse<ReviewerActivityItem[]>>(
      `/admin/projects/department/activity${limit ? `?limit=${limit}` : ''}`
    )
  ).data;
}

export async function getReviewerMySummary() {
  return (await apiClient<ApiResponse<ReviewerMySummary>>('/admin/projects/department/me')).data;
}

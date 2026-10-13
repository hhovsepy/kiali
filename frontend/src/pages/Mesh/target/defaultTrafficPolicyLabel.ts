import type { MeshDefaultTrafficPolicy } from 'types/IstioObjects';

export const defaultTrafficPolicySummary = (policy: MeshDefaultTrafficPolicy): string => {
  const parts: string[] = [];
  if (policy.connectionPool) {
    parts.push('Connection pool');
  }
  if (policy.outlierDetection) {
    parts.push('Outlier detection');
  }
  return parts.join(', ');
};

export const isDefaultTrafficPolicyConfigured = (policy?: MeshDefaultTrafficPolicy): boolean =>
  !!policy && (!!policy.connectionPool || !!policy.outlierDetection);

import { Label, Tooltip, TooltipPosition } from '@patternfly/react-core';
import { KialiIcon } from 'config/KialiIcon';
import * as React from 'react';
import { PFSpacer } from 'styles/PfSpacer';
import { kialiStyle } from 'styles/StyleUtils';
import type { MeshDefaultTrafficPolicy, OutboundTrafficPolicy } from 'types/IstioObjects';
import type { ControlPlaneMetricsMap } from 'types/Metrics';
import { useKialiTranslation } from 'utils/I18nUtils';
import { defaultTrafficPolicySummary, isDefaultTrafficPolicyConfigured } from './defaultTrafficPolicyLabel';
import { controlPlaneStatusLabelStyle, controlPlaneStatusStyle } from './TargetPanelCommon';

const statusRowStyle = kialiStyle({
  alignItems: 'center',
  display: 'flex',
  gap: PFSpacer.xs
});

const lockIconStyle = kialiStyle({ marginLeft: '0.25rem' });

type Props = {
  controlPlaneMetrics?: ControlPlaneMetricsMap;
  defaultTrafficPolicy?: MeshDefaultTrafficPolicy;
  minTLS?: string;
  outboundTrafficPolicy?: OutboundTrafficPolicy;
};

export const TargetPanelControlPlaneStatus: React.FC<Props> = (props: Props) => {
  const { t } = useKialiTranslation();

  const proxyTime = props.controlPlaneMetrics?.istiod_proxy_time;
  const proxySyncTime =
    proxyTime && proxyTime.length > 0
      ? proxyTime[0].datapoints.reduce((a, b) => (a[1] < b[1] ? a : b))[1] * 1000
      : undefined;
  const proxySyncTimeStr = !proxySyncTime || isNaN(proxySyncTime) ? 'N/A' : `${proxySyncTime.toFixed(2)}ms`;
  const defaultTrafficPolicy = isDefaultTrafficPolicyConfigured(props.defaultTrafficPolicy)
    ? props.defaultTrafficPolicy
    : undefined;

  return (
    <div className={controlPlaneStatusStyle}>
      {defaultTrafficPolicy && (
        <div className={statusRowStyle}>
          <div className={controlPlaneStatusLabelStyle}>{t('Default traffic policy')}</div>
          <Tooltip
            position={TooltipPosition.right}
            content={
              <div style={{ textAlign: 'left' }}>
                {t(
                  `This shows meshConfig.defaultTrafficPolicy. DestinationRules inherit these baseline connection pool and \
outlier detection settings unless they define their own trafficPolicy.`
                )}
              </div>
            }
          >
            <Label isCompact color="blue" data-test="mesh-default-traffic-policy">
              {defaultTrafficPolicySummary(defaultTrafficPolicy)}
            </Label>
          </Tooltip>
        </div>
      )}
      {props.outboundTrafficPolicy && (
        <div className={statusRowStyle}>
          <div className={controlPlaneStatusLabelStyle}>{t('Outbound policy')}</div>
          <Tooltip
            position={TooltipPosition.right}
            content={
              <div style={{ textAlign: 'left' }}>
                {t(`This shows the meshConfig.outboundTrafficPolicy.mode setting. It controls the sidecar handling of \
                requests to external services (services not defined in Istio’s internal service registry). When \
                set to ALLOW_ANY the Istio proxy performs a passthrough to the unknown services. When set to REGISTRY_ONLY \
                the Istio proxy blocks requets to hosts without a defined HTTP service or service entry.`)}
              </div>
            }
          >
            <Label isCompact color="blue">
              {props.outboundTrafficPolicy.mode}
            </Label>
          </Tooltip>
        </div>
      )}
      <div className={statusRowStyle}>
        <div className={controlPlaneStatusLabelStyle}>{t('Proxy sync time')}</div>
        <Tooltip
          position={TooltipPosition.right}
          content={
            <div style={{ textAlign: 'left' }}>
              {t(`The control plane must sync each proxy when there is a configuration change. This is the average time spent, per proxy, \
                 to perform the update. N/A means there was no syncing performed for the configured time period.`)}
            </div>
          }
        >
          <Label isCompact color="blue">
            {proxySyncTimeStr}
          </Label>
        </Tooltip>
      </div>
      {props.minTLS && (
        <div className={statusRowStyle}>
          <div className={controlPlaneStatusLabelStyle}>{t('Min TLS version')}</div>
          <Tooltip
            position={TooltipPosition.right}
            content={
              <div style={{ textAlign: 'left' }}>
                {t(
                  `The minimum TLS version for connections among Istio workloads. Defaults to TLSV1_2 if not set. Set by the 'meshConfig.meshMTLS.minProtocolVersion' field.`
                )}
              </div>
            }
          >
            <Label isCompact color="blue" data-test="label-TLS">
              <div style={{ display: '-webkit-box' }}>
                {props.minTLS} <KialiIcon.MtlsLock className={lockIconStyle} />
              </div>
            </Label>
          </Tooltip>
        </div>
      )}
    </div>
  );
};

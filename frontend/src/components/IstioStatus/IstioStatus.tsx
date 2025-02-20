import * as React from 'react';
import { SVGIconProps } from '@patternfly/react-icons/dist/js/createIcon';
import * as API from '../../services/Api';
import * as AlertUtils from '../../utils/AlertUtils';
import { TimeInMilliseconds } from '../../types/Common';
import { ComponentStatus, Status } from '../../types/IstioStatus';
import { MessageType } from '../../types/MessageCenter';
import { Namespace } from '../../types/Namespace';
import { KialiAppState } from '../../store/Store';
import { istioStatusSelector, namespaceItemsSelector } from '../../store/Selectors';
import { IstioStatusActions } from '../../actions/IstioStatusActions';
import { connect } from 'react-redux';
import { Text, TextVariants, TextContent, Tooltip, TooltipPosition, Label } from '@patternfly/react-core';
import { IstioStatusList } from './IstioStatusList';
import { PFColors } from '../Pf/PfColors';
import {
  CheckCircleIcon,
  ExclamationCircleIcon,
  ExclamationTriangleIcon,
  InfoCircleIcon,
  QuestionCircleIcon
} from '@patternfly/react-icons';
import { KialiDispatch } from 'types/Redux';
import { NamespaceThunkActions } from '../../actions/NamespaceThunkActions';
import { connectRefresh } from '../Refresh/connectRefresh';
import { kialiStyle } from 'styles/StyleUtils';
import { IconProps, createIcon, KialiIcon } from 'config/KialiIcon';
import { Link } from 'react-router-dom-v5-compat';
import { useKialiTranslation } from 'utils/I18nUtils';
import { MASTHEAD } from 'components/Nav/Masthead/Masthead';
import { isControlPlaneAccessible } from '../../utils/MeshUtils';
import { homeCluster, serverConfig } from '../../config';
import { PFBadge, PFBadges } from '../Pf/PfBadges';

export type ClusterStatusMap = { [cluster: string]: ComponentStatus[] };

type ReduxStateProps = {
  namespaces?: Namespace[];
  statusMap: ClusterStatusMap; // map of clusters to ComponentStatus[]
};

type ReduxDispatchProps = {
  refreshNamespaces: () => void;
  setIstioStatus: (cluster: string, istioStatus: ComponentStatus[]) => void;
};

type StatusIcons = {
  ErrorIcon?: React.ComponentClass<SVGIconProps>;
  HealthyIcon?: React.ComponentClass<SVGIconProps>;
  InfoIcon?: React.ComponentClass<SVGIconProps>;
  WarningIcon?: React.ComponentClass<SVGIconProps>;
};

type Props = ReduxStateProps &
  ReduxDispatchProps & {
    cluster?: string;
    icons?: StatusIcons;
    lastRefreshAt: TimeInMilliseconds;
    location?: string;
  };

const ValidToColor = {
  'true-true-true': PFColors.Danger,
  'true-true-false': PFColors.Danger,
  'true-false-true': PFColors.Danger,
  'true-false-false': PFColors.Danger,
  'false-true-true': PFColors.Warning,
  'false-true-false': PFColors.Warning,
  'false-false-true': PFColors.Info,
  'false-false-false': PFColors.Success
};

const defaultIcons = {
  ErrorIcon: ExclamationCircleIcon,
  HealthyIcon: CheckCircleIcon,
  InfoIcon: InfoCircleIcon,
  WarningIcon: ExclamationTriangleIcon
};

const iconStyle = kialiStyle({
  marginLeft: '0.5rem',
  fontSize: '1rem'
});

const clusterStyle = kialiStyle({
  display: 'flex',
  alignItems: 'center'
});

const labelStyle = kialiStyle({
  $nest: {
    '& .pf-v5-c-label__icon': {
      marginRight: '0.5rem'
    }
  }
});

export const meshLinkStyle = kialiStyle({
  display: 'flex',
  justifyContent: 'center',
  marginTop: '0.75rem',
  $nest: {
    '& > span': {
      marginRight: '0.5rem'
    }
  }
});

export const IstioStatusComponent: React.FC<Props> = (props: Props) => {
  const { t } = useKialiTranslation();

  const { cluster, namespaces, setIstioStatus, refreshNamespaces, lastRefreshAt } = props;

  React.useEffect(() => {
    refreshNamespaces();
  }, [refreshNamespaces]);

  const fetchStatus = React.useCallback(
    (cl: string): void => {
      API.getIstioStatus(cl)
        .then(response => {
          setIstioStatus(cl, response.data);
        })
        .catch(error => {
          // User without namespaces can't have access to mTLS information. Reduce severity to info.
          const informative = namespaces && namespaces.length < 1;

          if (informative) {
            AlertUtils.addError(t('Istio deployment status disabled.'), error, 'default', MessageType.INFO);
          } else {
            AlertUtils.addError(t('Error fetching Istio deployment status.'), error, 'default', MessageType.ERROR);
          }
        });
    },
    [namespaces, setIstioStatus, t]
  );

  React.useEffect(() => {
    // when cluster is set, retrieve only for this one, Overview page
    if (cluster) {
      fetchStatus(cluster);
    } else {
      Object.keys(serverConfig.clusters).forEach(cl => fetchStatus(cl));
    }
  }, [cluster, lastRefreshAt, fetchStatus]);

  const tooltipContent = (): React.ReactNode => {
    return (
      <>
        <TextContent style={{ color: PFColors.White }}>
          {cluster ? (
            <Text component={TextVariants.h4}>{t('Istio Components Status')}</Text>
          ) : (
            <Text component={TextVariants.h4}>{t('Cluster Status')}</Text>
          )}
          {cluster ? (
            // for Overview page
            <>
              <div className={clusterStyle}>
                <PFBadge badge={PFBadges.Cluster} size="sm" />
                {cluster}
                <span style={{ marginLeft: '0.25rem' }}>
                  <KialiIcon.Star />
                </span>
              </div>
              <IstioStatusList status={props.statusMap[cluster] || []} cluster={cluster} />
            </>
          ) : (
            // for Masthead
            Object.keys(props.statusMap).map(cl => (
              <>
                <div className={clusterStyle}>
                  <PFBadge badge={PFBadges.Cluster} size="sm" />
                  {cl}
                  {cl === homeCluster?.name && (
                    <span style={{ marginLeft: '0.25rem' }}>
                      <KialiIcon.Star />
                    </span>
                  )}
                </div>
                <IstioStatusList key={cl} status={props.statusMap[cl] || []} cluster={cl} />
              </>
            ))
          )}
          {!props.location?.endsWith('/mesh') && isControlPlaneAccessible() && (
            <div className={meshLinkStyle}>
              <span>{t('More info at')}</span>
              <Link to="/mesh">{t('Mesh page')}</Link>
            </div>
          )}
        </TextContent>
      </>
    );
  };

  const tooltipColor = (): string => {
    let coreUnhealthy = false;
    let addonUnhealthy = false;
    let notReady = false;
    // when cluster is set, this is in Overview page, otherwise masthead
    const values = cluster ? props.statusMap[cluster] ?? [] : Object.values(props.statusMap).flat();

    Object.keys(values ?? {}).forEach((compKey: string) => {
      const { status, is_core } = values[compKey];
      const isNotReady: boolean = status === Status.NotReady;
      const isUnhealthy: boolean = status !== Status.Healthy && !isNotReady;

      if (is_core) {
        coreUnhealthy = coreUnhealthy || isUnhealthy;
      } else {
        addonUnhealthy = addonUnhealthy || isUnhealthy;
      }

      notReady = notReady || isNotReady;
    });

    return ValidToColor[`${coreUnhealthy}-${addonUnhealthy}-${notReady}`];
  };

  const healthyComponents = (): boolean => {
    // when cluster is set, this is in Overview page, otherwise masthead
    const values = cluster ? props.statusMap[cluster] ?? [] : Object.values(props.statusMap).flat();
    return values.reduce((healthy: boolean, compStatus: ComponentStatus) => {
      return healthy && compStatus.status === Status.Healthy;
    }, true);
  };

  const tooltipPosition = props.location === MASTHEAD ? TooltipPosition.bottom : TooltipPosition.top;

  let statusIcon: React.ReactElement;

  if (!healthyComponents()) {
    const icons = props.icons ? { ...defaultIcons, ...props.icons } : defaultIcons;
    const iconColor = tooltipColor();
    let icon = QuestionCircleIcon;
    let dataTest = 'istio-status';

    if (iconColor === PFColors.Danger) {
      icon = icons.ErrorIcon;
      dataTest = `${dataTest}-danger`;
    } else if (iconColor === PFColors.Warning) {
      icon = icons.WarningIcon;
      dataTest = `${dataTest}-warning`;
    } else if (iconColor === PFColors.Info) {
      icon = icons.InfoIcon;
      dataTest = `${dataTest}-info`;
    } else if (iconColor === PFColors.Success) {
      icon = icons.HealthyIcon;
      dataTest = `${dataTest}-success`;
    }

    const iconProps: IconProps = {
      className: iconStyle,
      dataTest: dataTest
    };

    statusIcon = createIcon(iconProps, icon, iconColor);
  } else if (!cluster) {
    const iconProps: IconProps = {
      className: iconStyle,
      dataTest: 'istio-status-success'
    };

    statusIcon = createIcon(iconProps, defaultIcons.HealthyIcon, ValidToColor['false-false-false']);
  } else {
    return null;
  }

  return (
    <Tooltip position={tooltipPosition} enableFlip={true} content={tooltipContent()} maxWidth="25rem">
      <>
        {homeCluster?.name && (
          <Label className={labelStyle} data-test="cluster-icon" color="blue" icon={<KialiIcon.Cluster />}>
            {homeCluster?.name}
            {isControlPlaneAccessible() && statusIcon}
          </Label>
        )}
      </>
    </Tooltip>
  );
};

const mapStateToProps = (state: KialiAppState): ReduxStateProps => ({
  statusMap: istioStatusSelector(state),
  namespaces: namespaceItemsSelector(state)
});

const mapDispatchToProps = (dispatch: KialiDispatch): ReduxDispatchProps => ({
  setIstioStatus: (cluster: string, istioStatus: ComponentStatus[]) => {
    dispatch(IstioStatusActions.setinfo({ cluster, istioStatus }));
  },
  refreshNamespaces: () => {
    dispatch(NamespaceThunkActions.fetchNamespacesIfNeeded());
  }
});

export const IstioStatus = connectRefresh(connect(mapStateToProps, mapDispatchToProps)(IstioStatusComponent));

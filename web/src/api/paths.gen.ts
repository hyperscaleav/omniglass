// Generated from api/openapi.json by web/scripts/gen-api-paths.mjs (make gen). Do not edit by hand.
import type * as og from "./api.gen";

export interface paths {
  "/action-rules": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listActionRuleResponseSuccess["data"];
      error: og.listActionRuleResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createActionRule>[0]>;
      bodyRequired: true;
      data: og.createActionRuleResponseSuccess["data"];
      error: og.createActionRuleResponseError["data"];
    };
  };
  "/action-rules/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getActionRuleResponseSuccess["data"];
      error: og.getActionRuleResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteActionRuleResponseSuccess["data"];
      error: og.deleteActionRuleResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateActionRule>[1]>;
      bodyRequired: true;
      data: og.updateActionRuleResponseSuccess["data"];
      error: og.updateActionRuleResponseError["data"];
    };
  };
  "/action-rules/{name}:optIn": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.optInActionRuleResponseSuccess["data"];
      error: og.optInActionRuleResponseError["data"];
    };
  };
  "/action-rules/{name}:optOut": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.optOutActionRuleResponseSuccess["data"];
      error: og.optOutActionRuleResponseError["data"];
    };
  };
  "/audit-log": {
    get: {
      path: never;
      query: NonNullable<Parameters<typeof og.listAuditLog>[0]>;
      body: never;
      bodyRequired: false;
      data: og.listAuditLogResponseSuccess["data"];
      error: og.listAuditLogResponseError["data"];
    };
  };
  "/auth/login": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.login>[0]>;
      bodyRequired: true;
      data: og.loginResponseSuccess["data"];
      error: og.loginResponseError["data"];
    };
  };
  "/auth/logout": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.logoutResponseSuccess["data"];
      error: og.logoutResponseError["data"];
    };
  };
  "/auth/me": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getAuthMeResponseSuccess["data"];
      error: og.getAuthMeResponseError["data"];
    };
    patch: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.updateAuthMe>[0]>;
      bodyRequired: true;
      data: og.updateAuthMeResponseSuccess["data"];
      error: og.updateAuthMeResponseError["data"];
    };
  };
  "/auth/me/action-rules": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listAuthMeActionRulesResponseSuccess["data"];
      error: og.listAuthMeActionRulesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createAuthMeActionRule>[0]>;
      bodyRequired: true;
      data: og.createAuthMeActionRuleResponseSuccess["data"];
      error: og.createAuthMeActionRuleResponseError["data"];
    };
  };
  "/auth/me/action-rules/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getAuthMeActionRuleResponseSuccess["data"];
      error: og.getAuthMeActionRuleResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteAuthMeActionRuleResponseSuccess["data"];
      error: og.deleteAuthMeActionRuleResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateAuthMeActionRule>[1]>;
      bodyRequired: true;
      data: og.updateAuthMeActionRuleResponseSuccess["data"];
      error: og.updateAuthMeActionRuleResponseError["data"];
    };
  };
  "/auth/me/action-rules/{name}:test": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.testAuthMeActionRule>[1]>;
      bodyRequired: false;
      data: og.testAuthMeActionRuleResponseSuccess["data"];
      error: og.testAuthMeActionRuleResponseError["data"];
    };
  };
  "/auth/me/action-rules:testDraft": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.testAuthMeActionRulesDraft>[0]>;
      bodyRequired: false;
      data: og.testAuthMeActionRulesDraftResponseSuccess["data"];
      error: og.testAuthMeActionRulesDraftResponseError["data"];
    };
  };
  "/auth/me/avatar": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getAuthMeAvatarResponseSuccess["data"];
      error: og.getAuthMeAvatarResponseError["data"];
    };
  };
  "/auth/me/notifications": {
    get: {
      path: never;
      query: NonNullable<Parameters<typeof og.listAuthMeNotifications>[0]>;
      body: never;
      bodyRequired: false;
      data: og.listAuthMeNotificationsResponseSuccess["data"];
      error: og.listAuthMeNotificationsResponseError["data"];
    };
  };
  "/auth/me/notifications/{id}:markRead": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.markReadAuthMeNotificationResponseSuccess["data"];
      error: og.markReadAuthMeNotificationResponseError["data"];
    };
  };
  "/auth/me/notifications:markAllRead": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.markAllReadAuthMeNotificationsResponseSuccess["data"];
      error: og.markAllReadAuthMeNotificationsResponseError["data"];
    };
  };
  "/auth/me/sessions": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listAuthMeSessionsResponseSuccess["data"];
      error: og.listAuthMeSessionsResponseError["data"];
    };
  };
  "/auth/me/sessions/{id}:revoke": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revokeAuthMeSessionResponseSuccess["data"];
      error: og.revokeAuthMeSessionResponseError["data"];
    };
  };
  "/auth/me/sessions:revokeAll": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.revokeAllAuthMeSessions>[0]>;
      bodyRequired: true;
      data: og.revokeAllAuthMeSessionsResponseSuccess["data"];
      error: og.revokeAllAuthMeSessionsResponseError["data"];
    };
  };
  "/auth/me/tokens": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createAuthMeToken>[0]>;
      bodyRequired: true;
      data: og.createAuthMeTokenResponseSuccess["data"];
      error: og.createAuthMeTokenResponseError["data"];
    };
  };
  "/auth/me:changePassword": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.changeAuthMePassword>[0]>;
      bodyRequired: true;
      data: og.changeAuthMePasswordResponseSuccess["data"];
      error: og.changeAuthMePasswordResponseError["data"];
    };
  };
  "/auth/me:removeAvatar": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.removeAuthMeAvatarResponseSuccess["data"];
      error: og.removeAuthMeAvatarResponseError["data"];
    };
  };
  "/auth/me:setAvatar": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.setAuthMeAvatar>[0]>;
      bodyRequired: true;
      data: og.setAuthMeAvatarResponseSuccess["data"];
      error: og.setAuthMeAvatarResponseError["data"];
    };
  };
  "/auth/me:stopImpersonation": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.stopImpersonationResponseSuccess["data"];
      error: og.stopImpersonationResponseError["data"];
    };
  };
  "/auth/status": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getAuthStatusResponseSuccess["data"];
      error: og.getAuthStatusResponseError["data"];
    };
  };
  "/calc-rules": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listCalcRuleResponseSuccess["data"];
      error: og.listCalcRuleResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createCalcRule>[0]>;
      bodyRequired: true;
      data: og.createCalcRuleResponseSuccess["data"];
      error: og.createCalcRuleResponseError["data"];
    };
  };
  "/calc-rules/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getCalcRuleResponseSuccess["data"];
      error: og.getCalcRuleResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteCalcRuleResponseSuccess["data"];
      error: og.deleteCalcRuleResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateCalcRule>[1]>;
      bodyRequired: true;
      data: og.updateCalcRuleResponseSuccess["data"];
      error: og.updateCalcRuleResponseError["data"];
    };
  };
  "/calc-rules/{name}/state": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.getCalcRuleState>[1]>;
      body: never;
      bodyRequired: false;
      data: og.getCalcRuleStateResponseSuccess["data"];
      error: og.getCalcRuleStateResponseError["data"];
    };
  };
  "/calc-rules/{name}:rename": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameCalcRule>[1]>;
      bodyRequired: true;
      data: og.renameCalcRuleResponseSuccess["data"];
      error: og.renameCalcRuleResponseError["data"];
    };
  };
  "/calc-rules:preview": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.previewCalcRule>[0]>;
      bodyRequired: true;
      data: og.previewCalcRuleResponseSuccess["data"];
      error: og.previewCalcRuleResponseError["data"];
    };
  };
  "/command-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listCommandTypeResponseSuccess["data"];
      error: og.listCommandTypeResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createCommandType>[0]>;
      bodyRequired: true;
      data: og.createCommandTypeResponseSuccess["data"];
      error: og.createCommandTypeResponseError["data"];
    };
  };
  "/command-types/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getCommandTypeResponseSuccess["data"];
      error: og.getCommandTypeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteCommandTypeResponseSuccess["data"];
      error: og.deleteCommandTypeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateCommandType>[1]>;
      bodyRequired: true;
      data: og.updateCommandTypeResponseSuccess["data"];
      error: og.updateCommandTypeResponseError["data"];
    };
  };
  "/component-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentTypesResponseSuccess["data"];
      error: og.listComponentTypesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createComponentType>[0]>;
      bodyRequired: true;
      data: og.createComponentTypeResponseSuccess["data"];
      error: og.createComponentTypeResponseError["data"];
    };
  };
  "/component-types/{id}": {
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteComponentTypeResponseSuccess["data"];
      error: og.deleteComponentTypeResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateComponentType>[1]>;
      bodyRequired: true;
      data: og.updateComponentTypeResponseSuccess["data"];
      error: og.updateComponentTypeResponseError["data"];
    };
  };
  "/component-types/{id}:restore": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.restoreComponentTypeResponseSuccess["data"];
      error: og.restoreComponentTypeResponseError["data"];
    };
  };
  "/components": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentsResponseSuccess["data"];
      error: og.listComponentsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createComponent>[0]>;
      bodyRequired: true;
      data: og.createComponentResponseSuccess["data"];
      error: og.createComponentResponseError["data"];
    };
  };
  "/components/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentResponseSuccess["data"];
      error: og.getComponentResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteComponentResponseSuccess["data"];
      error: og.deleteComponentResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateComponent>[1]>;
      bodyRequired: true;
      data: og.updateComponentResponseSuccess["data"];
      error: og.updateComponentResponseError["data"];
    };
  };
  "/components/{name}/alarms": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.listComponentAlarms>[1]>;
      body: never;
      bodyRequired: false;
      data: og.listComponentAlarmsResponseSuccess["data"];
      error: og.listComponentAlarmsResponseError["data"];
    };
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.raiseComponentAlarm>[1]>;
      bodyRequired: true;
      data: og.raiseComponentAlarmResponseSuccess["data"];
      error: og.raiseComponentAlarmResponseError["data"];
    };
  };
  "/components/{name}/alarms/{id}": {
    delete: {
      path: { "name": string; "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.clearComponentAlarmResponseSuccess["data"];
      error: og.clearComponentAlarmResponseError["data"];
    };
  };
  "/components/{name}/alarms/{id}:acknowledge": {
    post: {
      path: { "name": string; "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.acknowledgeComponentAlarmResponseSuccess["data"];
      error: og.acknowledgeComponentAlarmResponseError["data"];
    };
  };
  "/components/{name}/alarms/{id}:snooze": {
    post: {
      path: { "name": string; "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.snoozeComponentAlarm>[2]>;
      bodyRequired: true;
      data: og.snoozeComponentAlarmResponseSuccess["data"];
      error: og.snoozeComponentAlarmResponseError["data"];
    };
  };
  "/components/{name}/alarms/{id}:timeline": {
    get: {
      path: { "name": string; "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentAlarmTimelineResponseSuccess["data"];
      error: og.getComponentAlarmTimelineResponseError["data"];
    };
  };
  "/components/{name}/alarms/{id}:unsnooze": {
    post: {
      path: { "name": string; "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.unsnoozeComponentAlarmResponseSuccess["data"];
      error: og.unsnoozeComponentAlarmResponseError["data"];
    };
  };
  "/components/{name}/commands": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.listComponentCommands>[1]>;
      body: never;
      bodyRequired: false;
      data: og.listComponentCommandsResponseSuccess["data"];
      error: og.listComponentCommandsResponseError["data"];
    };
  };
  "/components/{name}/commands/{id}": {
    get: {
      path: { "name": string; "id": number };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentCommandResponseSuccess["data"];
      error: og.getComponentCommandResponseError["data"];
    };
  };
  "/components/{name}/commands:issue": {
    post: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.issueComponentCommand>[2]>;
      body: NonNullable<Parameters<typeof og.issueComponentCommand>[1]>;
      bodyRequired: true;
      data: og.issueComponentCommandResponseSuccess["data"];
      error: og.issueComponentCommandResponseError["data"];
    };
  };
  "/components/{name}/dependencies": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentDependenciesResponseSuccess["data"];
      error: og.listComponentDependenciesResponseError["data"];
    };
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.createComponentDependency>[1]>;
      bodyRequired: true;
      data: og.createComponentDependencyResponseSuccess["data"];
      error: og.createComponentDependencyResponseError["data"];
    };
  };
  "/components/{name}/dependencies/{kind}/{upstream}": {
    delete: {
      path: { "name": string; "kind": string; "upstream": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteComponentDependencyResponseSuccess["data"];
      error: og.deleteComponentDependencyResponseError["data"];
    };
  };
  "/components/{name}/effective-secrets": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.effectiveSecretsResponseSuccess["data"];
      error: og.effectiveSecretsResponseError["data"];
    };
  };
  "/components/{name}/effective-tags": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.effectiveTags>[1]>;
      body: never;
      bodyRequired: false;
      data: og.effectiveTagsResponseSuccess["data"];
      error: og.effectiveTagsResponseError["data"];
    };
  };
  "/components/{name}/effective-variables": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.effectiveVariablesResponseSuccess["data"];
      error: og.effectiveVariablesResponseError["data"];
    };
  };
  "/components/{name}/events": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentEventsResponseSuccess["data"];
      error: og.listComponentEventsResponseError["data"];
    };
  };
  "/components/{name}/identity-bindings": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listIdentityBindingsResponseSuccess["data"];
      error: og.listIdentityBindingsResponseError["data"];
    };
  };
  "/components/{name}/identity-bindings/{key}/{value}": {
    put: {
      path: { "name": string; "key": string; "value": string };
      query: never;
      body: NonNullable<Parameters<typeof og.putIdentityBinding>[3]>;
      bodyRequired: true;
      data: og.putIdentityBindingResponseSuccess["data"];
      error: og.putIdentityBindingResponseError["data"];
    };
    delete: {
      path: { "name": string; "key": string; "value": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteIdentityBindingResponseSuccess["data"];
      error: og.deleteIdentityBindingResponseError["data"];
    };
  };
  "/components/{name}/impairment": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentImpairmentResponseSuccess["data"];
      error: og.getComponentImpairmentResponseError["data"];
    };
    put: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setComponentImpairment>[1]>;
      bodyRequired: true;
      data: og.setComponentImpairmentResponseSuccess["data"];
      error: og.setComponentImpairmentResponseError["data"];
    };
  };
  "/components/{name}/logs": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.listComponentLogs>[1]>;
      body: never;
      bodyRequired: false;
      data: og.listComponentLogsResponseSuccess["data"];
      error: og.listComponentLogsResponseError["data"];
    };
  };
  "/components/{name}/memberships": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentMembershipsResponseSuccess["data"];
      error: og.listComponentMembershipsResponseError["data"];
    };
  };
  "/components/{name}/metrics": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentMetricsResponseSuccess["data"];
      error: og.listComponentMetricsResponseError["data"];
    };
  };
  "/components/{name}/metrics/{metric}/samples": {
    get: {
      path: { "name": string; "metric": string };
      query: NonNullable<Parameters<typeof og.listComponentMetricSeries>[2]>;
      body: never;
      bodyRequired: false;
      data: og.listComponentMetricSeriesResponseSuccess["data"];
      error: og.listComponentMetricSeriesResponseError["data"];
    };
  };
  "/components/{name}/properties": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentPropertiesResponseSuccess["data"];
      error: og.listComponentPropertiesResponseError["data"];
    };
  };
  "/components/{name}/properties/{property}": {
    put: {
      path: { "name": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setComponentProperty>[2]>;
      bodyRequired: true;
      data: og.setComponentPropertyResponseSuccess["data"];
      error: og.setComponentPropertyResponseError["data"];
    };
    delete: {
      path: { "name": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.clearComponentPropertyResponseSuccess["data"];
      error: og.clearComponentPropertyResponseError["data"];
    };
  };
  "/components/{name}/properties/{property}/samples": {
    get: {
      path: { "name": string; "property": string };
      query: NonNullable<Parameters<typeof og.listComponentPropertySeries>[2]>;
      body: never;
      bodyRequired: false;
      data: og.listComponentPropertySeriesResponseSuccess["data"];
      error: og.listComponentPropertySeriesResponseError["data"];
    };
  };
  "/components/{name}/reachability": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentReachabilityResponseSuccess["data"];
      error: og.getComponentReachabilityResponseError["data"];
    };
  };
  "/components/{name}/simulation": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getComponentSimulationResponseSuccess["data"];
      error: og.getComponentSimulationResponseError["data"];
    };
  };
  "/components/{name}/simulation:fire": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.fireComponentSimulation>[1]>;
      bodyRequired: true;
      data: og.fireComponentSimulationResponseSuccess["data"];
      error: og.fireComponentSimulationResponseError["data"];
    };
  };
  "/components/{name}/unbound-identities": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listUnboundIdentitiesResponseSuccess["data"];
      error: og.listUnboundIdentitiesResponseError["data"];
    };
  };
  "/components/{name}:listTags": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listComponentTagsResponseSuccess["data"];
      error: og.listComponentTagsResponseError["data"];
    };
  };
  "/components/{name}:move": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.moveComponent>[1]>;
      bodyRequired: true;
      data: og.moveComponentResponseSuccess["data"];
      error: og.moveComponentResponseError["data"];
    };
  };
  "/components/{name}:removeTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.removeComponentTag>[1]>;
      bodyRequired: true;
      data: og.removeComponentTagResponseSuccess["data"];
      error: og.removeComponentTagResponseError["data"];
    };
  };
  "/components/{name}:rename": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameComponent>[1]>;
      bodyRequired: true;
      data: og.renameComponentResponseSuccess["data"];
      error: og.renameComponentResponseError["data"];
    };
  };
  "/components/{name}:resetName": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.resetComponentNameResponseSuccess["data"];
      error: og.resetComponentNameResponseError["data"];
    };
  };
  "/components/{name}:setTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setComponentTag>[1]>;
      bodyRequired: true;
      data: og.setComponentTagResponseSuccess["data"];
      error: og.setComponentTagResponseError["data"];
    };
  };
  "/components:checkName": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.checkComponentName>[0]>;
      bodyRequired: true;
      data: og.checkComponentNameResponseSuccess["data"];
      error: og.checkComponentNameResponseError["data"];
    };
  };
  "/components:createFromTemplate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createComponentFromTemplate>[0]>;
      bodyRequired: true;
      data: og.createComponentFromTemplateResponseSuccess["data"];
      error: og.createComponentFromTemplateResponseError["data"];
    };
  };
  "/components:previewLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.previewComponentLabelsResponseSuccess["data"];
      error: og.previewComponentLabelsResponseError["data"];
    };
  };
  "/components:recomputeLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.recomputeComponentLabelsResponseSuccess["data"];
      error: og.recomputeComponentLabelsResponseError["data"];
    };
  };
  "/components:renderLabel": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.renderComponentLabel>[0]>;
      bodyRequired: true;
      data: og.renderComponentLabelResponseSuccess["data"];
      error: og.renderComponentLabelResponseError["data"];
    };
  };
  "/drivers": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listDriversResponseSuccess["data"];
      error: og.listDriversResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createDriver>[0]>;
      bodyRequired: true;
      data: og.createDriverResponseSuccess["data"];
      error: og.createDriverResponseError["data"];
    };
  };
  "/drivers/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getDriverResponseSuccess["data"];
      error: og.getDriverResponseError["data"];
    };
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteDriverResponseSuccess["data"];
      error: og.deleteDriverResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateDriver>[1]>;
      bodyRequired: true;
      data: og.updateDriverResponseSuccess["data"];
      error: og.updateDriverResponseError["data"];
    };
  };
  "/drivers/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameDriver>[1]>;
      bodyRequired: true;
      data: og.renameDriverResponseSuccess["data"];
      error: og.renameDriverResponseError["data"];
    };
  };
  "/drivers:preview": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.previewDriver>[0]>;
      bodyRequired: true;
      data: og.previewDriverResponseSuccess["data"];
      error: og.previewDriverResponseError["data"];
    };
  };
  "/drivers:validate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.validateDriverSpec>[0]>;
      bodyRequired: true;
      data: og.validateDriverSpecResponseSuccess["data"];
      error: og.validateDriverSpecResponseError["data"];
    };
  };
  "/endpoints": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listEndpointsResponseSuccess["data"];
      error: og.listEndpointsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createEndpoint>[0]>;
      bodyRequired: true;
      data: og.createEndpointResponseSuccess["data"];
      error: og.createEndpointResponseError["data"];
    };
  };
  "/endpoints/{endpoint}": {
    get: {
      path: { "endpoint": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getEndpointResponseSuccess["data"];
      error: og.getEndpointResponseError["data"];
    };
    delete: {
      path: { "endpoint": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteEndpointResponseSuccess["data"];
      error: og.deleteEndpointResponseError["data"];
    };
    patch: {
      path: { "endpoint": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateEndpoint>[1]>;
      bodyRequired: true;
      data: og.updateEndpointResponseSuccess["data"];
      error: og.updateEndpointResponseError["data"];
    };
  };
  "/endpoints/{endpoint}/polls/{poll}:run": {
    post: {
      path: { "endpoint": string; "poll": string };
      query: NonNullable<Parameters<typeof og.runEndpointPoll>[2]>;
      body: never;
      bodyRequired: false;
      data: og.runEndpointPollResponseSuccess["data"];
      error: og.runEndpointPollResponseError["data"];
    };
  };
  "/endpoints/{endpoint}:retry": {
    post: {
      path: { "endpoint": string };
      query: never;
      body: NonNullable<Parameters<typeof og.retryEndpoint>[1]>;
      bodyRequired: false;
      data: og.retryEndpointResponseSuccess["data"];
      error: og.retryEndpointResponseError["data"];
    };
  };
  "/endpoints/{endpoint}:rotateToken": {
    post: {
      path: { "endpoint": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.rotateEndpointTokenResponseSuccess["data"];
      error: og.rotateEndpointTokenResponseError["data"];
    };
  };
  "/event-rules": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listEventRuleResponseSuccess["data"];
      error: og.listEventRuleResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createEventRule>[0]>;
      bodyRequired: true;
      data: og.createEventRuleResponseSuccess["data"];
      error: og.createEventRuleResponseError["data"];
    };
  };
  "/event-rules/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getEventRuleResponseSuccess["data"];
      error: og.getEventRuleResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteEventRuleResponseSuccess["data"];
      error: og.deleteEventRuleResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateEventRule>[1]>;
      bodyRequired: true;
      data: og.updateEventRuleResponseSuccess["data"];
      error: og.updateEventRuleResponseError["data"];
    };
  };
  "/event-rules/{name}/state": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getEventRuleStateResponseSuccess["data"];
      error: og.getEventRuleStateResponseError["data"];
    };
  };
  "/event-rules/{name}:rename": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameEventRule>[1]>;
      bodyRequired: true;
      data: og.renameEventRuleResponseSuccess["data"];
      error: og.renameEventRuleResponseError["data"];
    };
  };
  "/event-rules:preview": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.previewEventRule>[0]>;
      bodyRequired: true;
      data: og.previewEventRuleResponseSuccess["data"];
      error: og.previewEventRuleResponseError["data"];
    };
  };
  "/event-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listEventTypeResponseSuccess["data"];
      error: og.listEventTypeResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createEventType>[0]>;
      bodyRequired: true;
      data: og.createEventTypeResponseSuccess["data"];
      error: og.createEventTypeResponseError["data"];
    };
  };
  "/event-types/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getEventTypeResponseSuccess["data"];
      error: og.getEventTypeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteEventTypeResponseSuccess["data"];
      error: og.deleteEventTypeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateEventType>[1]>;
      bodyRequired: true;
      data: og.updateEventTypeResponseSuccess["data"];
      error: og.updateEventTypeResponseError["data"];
    };
  };
  "/expressions:validate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.validateExpression>[0]>;
      bodyRequired: true;
      data: og.validateExpressionResponseSuccess["data"];
      error: og.validateExpressionResponseError["data"];
    };
  };
  "/files": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listFilesResponseSuccess["data"];
      error: og.listFilesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createFile>[0]>;
      bodyRequired: true;
      data: og.createFileResponseSuccess["data"];
      error: og.createFileResponseError["data"];
    };
  };
  "/files/{file}": {
    get: {
      path: { "file": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getFileResponseSuccess["data"];
      error: og.getFileResponseError["data"];
    };
    delete: {
      path: { "file": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteFileResponseSuccess["data"];
      error: og.deleteFileResponseError["data"];
    };
  };
  "/files/{file}:download": {
    get: {
      path: { "file": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.downloadFileResponseSuccess["data"];
      error: og.downloadFileResponseError["data"];
    };
  };
  "/healthz": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getHealthzResponseSuccess["data"];
      error: og.getHealthzResponseError["data"];
    };
  };
  "/location-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationTypesResponseSuccess["data"];
      error: og.listLocationTypesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createLocationType>[0]>;
      bodyRequired: true;
      data: og.createLocationTypeResponseSuccess["data"];
      error: og.createLocationTypeResponseError["data"];
    };
  };
  "/location-types/{id}": {
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteLocationTypeResponseSuccess["data"];
      error: og.deleteLocationTypeResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateLocationType>[1]>;
      bodyRequired: true;
      data: og.updateLocationTypeResponseSuccess["data"];
      error: og.updateLocationTypeResponseError["data"];
    };
  };
  "/location-types/{id}/metrics": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationTypeMetricsResponseSuccess["data"];
      error: og.listLocationTypeMetricsResponseError["data"];
    };
  };
  "/location-types/{id}/metrics/{metric}": {
    put: {
      path: { "id": string; "metric": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setLocationTypeMetric>[2]>;
      bodyRequired: true;
      data: og.setLocationTypeMetricResponseSuccess["data"];
      error: og.setLocationTypeMetricResponseError["data"];
    };
    delete: {
      path: { "id": string; "metric": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteLocationTypeMetricResponseSuccess["data"];
      error: og.deleteLocationTypeMetricResponseError["data"];
    };
  };
  "/location-types/{id}/properties": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationTypePropertiesResponseSuccess["data"];
      error: og.listLocationTypePropertiesResponseError["data"];
    };
  };
  "/location-types/{id}/properties/{property}": {
    put: {
      path: { "id": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setLocationTypeProperty>[2]>;
      bodyRequired: true;
      data: og.setLocationTypePropertyResponseSuccess["data"];
      error: og.setLocationTypePropertyResponseError["data"];
    };
    delete: {
      path: { "id": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteLocationTypePropertyResponseSuccess["data"];
      error: og.deleteLocationTypePropertyResponseError["data"];
    };
  };
  "/location-types/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameLocationType>[1]>;
      bodyRequired: true;
      data: og.renameLocationTypeResponseSuccess["data"];
      error: og.renameLocationTypeResponseError["data"];
    };
  };
  "/location-types/{id}:restore": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.restoreLocationTypeResponseSuccess["data"];
      error: og.restoreLocationTypeResponseError["data"];
    };
  };
  "/locations": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationsResponseSuccess["data"];
      error: og.listLocationsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createLocation>[0]>;
      bodyRequired: true;
      data: og.createLocationResponseSuccess["data"];
      error: og.createLocationResponseError["data"];
    };
  };
  "/locations/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getLocationResponseSuccess["data"];
      error: og.getLocationResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteLocationResponseSuccess["data"];
      error: og.deleteLocationResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateLocation>[1]>;
      bodyRequired: true;
      data: og.updateLocationResponseSuccess["data"];
      error: og.updateLocationResponseError["data"];
    };
  };
  "/locations/{name}/health": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getLocationHealthResponseSuccess["data"];
      error: og.getLocationHealthResponseError["data"];
    };
  };
  "/locations/{name}/metrics": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationMetricsResponseSuccess["data"];
      error: og.listLocationMetricsResponseError["data"];
    };
  };
  "/locations/{name}/properties": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationPropertiesResponseSuccess["data"];
      error: og.listLocationPropertiesResponseError["data"];
    };
  };
  "/locations/{name}/properties/{property}": {
    put: {
      path: { "name": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setLocationProperty>[2]>;
      bodyRequired: true;
      data: og.setLocationPropertyResponseSuccess["data"];
      error: og.setLocationPropertyResponseError["data"];
    };
    delete: {
      path: { "name": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.clearLocationPropertyResponseSuccess["data"];
      error: og.clearLocationPropertyResponseError["data"];
    };
  };
  "/locations/{name}:listTags": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listLocationTagsResponseSuccess["data"];
      error: og.listLocationTagsResponseError["data"];
    };
  };
  "/locations/{name}:move": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.moveLocation>[1]>;
      bodyRequired: true;
      data: og.moveLocationResponseSuccess["data"];
      error: og.moveLocationResponseError["data"];
    };
  };
  "/locations/{name}:removeTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.removeLocationTag>[1]>;
      bodyRequired: true;
      data: og.removeLocationTagResponseSuccess["data"];
      error: og.removeLocationTagResponseError["data"];
    };
  };
  "/locations/{name}:rename": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameLocation>[1]>;
      bodyRequired: true;
      data: og.renameLocationResponseSuccess["data"];
      error: og.renameLocationResponseError["data"];
    };
  };
  "/locations/{name}:resetName": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.resetLocationNameResponseSuccess["data"];
      error: og.resetLocationNameResponseError["data"];
    };
  };
  "/locations/{name}:setTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setLocationTag>[1]>;
      bodyRequired: true;
      data: og.setLocationTagResponseSuccess["data"];
      error: og.setLocationTagResponseError["data"];
    };
  };
  "/locations:checkName": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.checkLocationName>[0]>;
      bodyRequired: true;
      data: og.checkLocationNameResponseSuccess["data"];
      error: og.checkLocationNameResponseError["data"];
    };
  };
  "/locations:createFromTemplate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createLocationsFromTemplate>[0]>;
      bodyRequired: true;
      data: og.createLocationsFromTemplateResponseSuccess["data"];
      error: og.createLocationsFromTemplateResponseError["data"];
    };
  };
  "/locations:previewLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.previewLocationLabelsResponseSuccess["data"];
      error: og.previewLocationLabelsResponseError["data"];
    };
  };
  "/locations:recomputeLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.recomputeLocationLabelsResponseSuccess["data"];
      error: og.recomputeLocationLabelsResponseError["data"];
    };
  };
  "/locations:renderLabel": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.renderLocationLabel>[0]>;
      bodyRequired: true;
      data: og.renderLocationLabelResponseSuccess["data"];
      error: og.renderLocationLabelResponseError["data"];
    };
  };
  "/metric-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listMetricTypeResponseSuccess["data"];
      error: og.listMetricTypeResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createMetricType>[0]>;
      bodyRequired: true;
      data: og.createMetricTypeResponseSuccess["data"];
      error: og.createMetricTypeResponseError["data"];
    };
  };
  "/metric-types/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getMetricTypeResponseSuccess["data"];
      error: og.getMetricTypeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteMetricTypeResponseSuccess["data"];
      error: og.deleteMetricTypeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateMetricType>[1]>;
      bodyRequired: true;
      data: og.updateMetricTypeResponseSuccess["data"];
      error: og.updateMetricTypeResponseError["data"];
    };
  };
  "/metric-types/{name}:setRetention": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setMetricTypeRetention>[1]>;
      bodyRequired: true;
      data: og.setMetricTypeRetentionResponseSuccess["data"];
      error: og.setMetricTypeRetentionResponseError["data"];
    };
  };
  "/nodes": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listNodesResponseSuccess["data"];
      error: og.listNodesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createNode>[0]>;
      bodyRequired: true;
      data: og.createNodeResponseSuccess["data"];
      error: og.createNodeResponseError["data"];
    };
  };
  "/nodes/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNodeResponseSuccess["data"];
      error: og.getNodeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteNodeResponseSuccess["data"];
      error: og.deleteNodeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateNode>[1]>;
      bodyRequired: true;
      data: og.updateNodeResponseSuccess["data"];
      error: og.updateNodeResponseError["data"];
    };
  };
  "/nodes/{name}/logs": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listNodeLogsResponseSuccess["data"];
      error: og.listNodeLogsResponseError["data"];
    };
  };
  "/nodes/{name}/simulation": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNodeSimulationResponseSuccess["data"];
      error: og.getNodeSimulationResponseError["data"];
    };
  };
  "/nodes/{name}/simulation/components": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.addNodeSimulationComponent>[1]>;
      bodyRequired: true;
      data: og.addNodeSimulationComponentResponseSuccess["data"];
      error: og.addNodeSimulationComponentResponseError["data"];
    };
  };
  "/nodes/{name}/simulation/observation": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNodeSimulationObservationResponseSuccess["data"];
      error: og.getNodeSimulationObservationResponseError["data"];
    };
  };
  "/nodes/{name}/simulation/systems": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.addNodeSimulationSystem>[1]>;
      bodyRequired: true;
      data: og.addNodeSimulationSystemResponseSuccess["data"];
      error: og.addNodeSimulationSystemResponseError["data"];
    };
  };
  "/nodes/{name}:enroll": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.enrollNodeResponseSuccess["data"];
      error: og.enrollNodeResponseError["data"];
    };
  };
  "/nodes/{name}:listTags": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listNodeTagsResponseSuccess["data"];
      error: og.listNodeTagsResponseError["data"];
    };
  };
  "/nodes/{name}:removeTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.removeNodeTag>[1]>;
      bodyRequired: true;
      data: og.removeNodeTagResponseSuccess["data"];
      error: og.removeNodeTagResponseError["data"];
    };
  };
  "/nodes/{name}:setTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setNodeTag>[1]>;
      bodyRequired: true;
      data: og.setNodeTagResponseSuccess["data"];
      error: og.setNodeTagResponseError["data"];
    };
  };
  "/nodes:claim": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.claimNode>[0]>;
      bodyRequired: true;
      data: og.claimNodeResponseSuccess["data"];
      error: og.claimNodeResponseError["data"];
    };
  };
  "/notification-targets": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listNotificationTargetResponseSuccess["data"];
      error: og.listNotificationTargetResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createNotificationTarget>[0]>;
      bodyRequired: true;
      data: og.createNotificationTargetResponseSuccess["data"];
      error: og.createNotificationTargetResponseError["data"];
    };
  };
  "/notification-targets/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNotificationTargetResponseSuccess["data"];
      error: og.getNotificationTargetResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteNotificationTargetResponseSuccess["data"];
      error: og.deleteNotificationTargetResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateNotificationTarget>[1]>;
      bodyRequired: true;
      data: og.updateNotificationTargetResponseSuccess["data"];
      error: og.updateNotificationTargetResponseError["data"];
    };
  };
  "/notification-targets/{name}:reveal": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revealNotificationTargetResponseSuccess["data"];
      error: og.revealNotificationTargetResponseError["data"];
    };
  };
  "/notification-targets/{name}:test": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.testNotificationTargetResponseSuccess["data"];
      error: og.testNotificationTargetResponseError["data"];
    };
  };
  "/notification-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listNotificationTypeResponseSuccess["data"];
      error: og.listNotificationTypeResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createNotificationType>[0]>;
      bodyRequired: true;
      data: og.createNotificationTypeResponseSuccess["data"];
      error: og.createNotificationTypeResponseError["data"];
    };
  };
  "/notification-types/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameNotificationType>[1]>;
      bodyRequired: true;
      data: og.renameNotificationTypeResponseSuccess["data"];
      error: og.renameNotificationTypeResponseError["data"];
    };
  };
  "/notification-types/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNotificationTypeResponseSuccess["data"];
      error: og.getNotificationTypeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteNotificationTypeResponseSuccess["data"];
      error: og.deleteNotificationTypeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateNotificationType>[1]>;
      bodyRequired: true;
      data: og.updateNotificationTypeResponseSuccess["data"];
      error: og.updateNotificationTypeResponseError["data"];
    };
  };
  "/notification-types/{name}:preview": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.previewNotificationType>[1]>;
      bodyRequired: true;
      data: og.previewNotificationTypeResponseSuccess["data"];
      error: og.previewNotificationTypeResponseError["data"];
    };
  };
  "/notifications": {
    get: {
      path: never;
      query: NonNullable<Parameters<typeof og.listNotification>[0]>;
      body: never;
      bodyRequired: false;
      data: og.listNotificationResponseSuccess["data"];
      error: og.listNotificationResponseError["data"];
    };
  };
  "/notifications/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getNotificationResponseSuccess["data"];
      error: og.getNotificationResponseError["data"];
    };
  };
  "/notifications/{id}:retry": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.retryNotificationResponseSuccess["data"];
      error: og.retryNotificationResponseError["data"];
    };
  };
  "/principal-groups": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listGroupsResponseSuccess["data"];
      error: og.listGroupsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createGroup>[0]>;
      bodyRequired: true;
      data: og.createGroupResponseSuccess["data"];
      error: og.createGroupResponseError["data"];
    };
  };
  "/principal-groups/{group}": {
    get: {
      path: { "group": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getGroupResponseSuccess["data"];
      error: og.getGroupResponseError["data"];
    };
    delete: {
      path: { "group": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteGroupResponseSuccess["data"];
      error: og.deleteGroupResponseError["data"];
    };
    patch: {
      path: { "group": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateGroup>[1]>;
      bodyRequired: true;
      data: og.updateGroupResponseSuccess["data"];
      error: og.updateGroupResponseError["data"];
    };
  };
  "/principal-groups/{group}/grants": {
    get: {
      path: { "group": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listGroupGrantsResponseSuccess["data"];
      error: og.listGroupGrantsResponseError["data"];
    };
    post: {
      path: { "group": string };
      query: never;
      body: NonNullable<Parameters<typeof og.createGroupGrant>[1]>;
      bodyRequired: true;
      data: og.createGroupGrantResponseSuccess["data"];
      error: og.createGroupGrantResponseError["data"];
    };
  };
  "/principal-groups/{group}/grants/{grantId}": {
    delete: {
      path: { "group": string; "grantId": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revokeGroupGrantResponseSuccess["data"];
      error: og.revokeGroupGrantResponseError["data"];
    };
  };
  "/principal-groups/{group}/members": {
    get: {
      path: { "group": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listGroupMembersResponseSuccess["data"];
      error: og.listGroupMembersResponseError["data"];
    };
    post: {
      path: { "group": string };
      query: never;
      body: NonNullable<Parameters<typeof og.addGroupMember>[1]>;
      bodyRequired: true;
      data: og.addGroupMemberResponseSuccess["data"];
      error: og.addGroupMemberResponseError["data"];
    };
  };
  "/principal-groups/{group}/members/{principal}": {
    delete: {
      path: { "group": string; "principal": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.removeGroupMemberResponseSuccess["data"];
      error: og.removeGroupMemberResponseError["data"];
    };
  };
  "/principal-groups/{group}:rename": {
    post: {
      path: { "group": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameGroup>[1]>;
      bodyRequired: true;
      data: og.renameGroupResponseSuccess["data"];
      error: og.renameGroupResponseError["data"];
    };
  };
  "/principals": {
    get: {
      path: never;
      query: NonNullable<Parameters<typeof og.listPrincipals>[0]>;
      body: never;
      bodyRequired: false;
      data: og.listPrincipalsResponseSuccess["data"];
      error: og.listPrincipalsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createPrincipal>[0]>;
      bodyRequired: true;
      data: og.createPrincipalResponseSuccess["data"];
      error: og.createPrincipalResponseError["data"];
    };
  };
  "/principals/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getPrincipalResponseSuccess["data"];
      error: og.getPrincipalResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updatePrincipal>[1]>;
      bodyRequired: true;
      data: og.updatePrincipalResponseSuccess["data"];
      error: og.updatePrincipalResponseError["data"];
    };
  };
  "/principals/{id}/avatar": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getPrincipalAvatarResponseSuccess["data"];
      error: og.getPrincipalAvatarResponseError["data"];
    };
  };
  "/principals/{id}/grants": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.createGrant>[1]>;
      bodyRequired: true;
      data: og.createGrantResponseSuccess["data"];
      error: og.createGrantResponseError["data"];
    };
  };
  "/principals/{id}/grants/{grantId}": {
    delete: {
      path: { "id": string; "grantId": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revokeGrantResponseSuccess["data"];
      error: og.revokeGrantResponseError["data"];
    };
  };
  "/principals/{id}/sessions": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listPrincipalSessionsResponseSuccess["data"];
      error: og.listPrincipalSessionsResponseError["data"];
    };
  };
  "/principals/{id}/sessions/{sid}:revoke": {
    post: {
      path: { "id": string; "sid": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revokePrincipalSessionResponseSuccess["data"];
      error: og.revokePrincipalSessionResponseError["data"];
    };
  };
  "/principals/{id}/sessions:revokeAll": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.revokeAllPrincipalSessions>[1]>;
      bodyRequired: true;
      data: og.revokeAllPrincipalSessionsResponseSuccess["data"];
      error: og.revokeAllPrincipalSessionsResponseError["data"];
    };
  };
  "/principals/{id}:archive": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.archivePrincipalResponseSuccess["data"];
      error: og.archivePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:disable": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.disablePrincipalResponseSuccess["data"];
      error: og.disablePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:enable": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.enablePrincipalResponseSuccess["data"];
      error: og.enablePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:impersonate": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.impersonatePrincipal>[1]>;
      bodyRequired: true;
      data: og.impersonatePrincipalResponseSuccess["data"];
      error: og.impersonatePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:purge": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.purgePrincipalResponseSuccess["data"];
      error: og.purgePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:removeAvatar": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.removePrincipalAvatarResponseSuccess["data"];
      error: og.removePrincipalAvatarResponseError["data"];
    };
  };
  "/principals/{id}:resetPassword": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.resetPrincipalPassword>[1]>;
      bodyRequired: true;
      data: og.resetPrincipalPasswordResponseSuccess["data"];
      error: og.resetPrincipalPasswordResponseError["data"];
    };
  };
  "/principals/{id}:restore": {
    post: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.restorePrincipalResponseSuccess["data"];
      error: og.restorePrincipalResponseError["data"];
    };
  };
  "/principals/{id}:setAvatar": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setPrincipalAvatar>[1]>;
      bodyRequired: true;
      data: og.setPrincipalAvatarResponseSuccess["data"];
      error: og.setPrincipalAvatarResponseError["data"];
    };
  };
  "/products": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listProductsResponseSuccess["data"];
      error: og.listProductsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createProduct>[0]>;
      bodyRequired: true;
      data: og.createProductResponseSuccess["data"];
      error: og.createProductResponseError["data"];
    };
  };
  "/products/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getProductResponseSuccess["data"];
      error: og.getProductResponseError["data"];
    };
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteProductResponseSuccess["data"];
      error: og.deleteProductResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateProduct>[1]>;
      bodyRequired: true;
      data: og.updateProductResponseSuccess["data"];
      error: og.updateProductResponseError["data"];
    };
  };
  "/products/{id}/metrics": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listProductMetricsResponseSuccess["data"];
      error: og.listProductMetricsResponseError["data"];
    };
  };
  "/products/{id}/metrics/{metric}": {
    put: {
      path: { "id": string; "metric": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setProductMetric>[2]>;
      bodyRequired: true;
      data: og.setProductMetricResponseSuccess["data"];
      error: og.setProductMetricResponseError["data"];
    };
    delete: {
      path: { "id": string; "metric": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteProductMetricResponseSuccess["data"];
      error: og.deleteProductMetricResponseError["data"];
    };
  };
  "/products/{id}/properties": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listProductPropertiesResponseSuccess["data"];
      error: og.listProductPropertiesResponseError["data"];
    };
  };
  "/products/{id}/properties/{property}": {
    put: {
      path: { "id": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setProductProperty>[2]>;
      bodyRequired: true;
      data: og.setProductPropertyResponseSuccess["data"];
      error: og.setProductPropertyResponseError["data"];
    };
    delete: {
      path: { "id": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteProductPropertyResponseSuccess["data"];
      error: og.deleteProductPropertyResponseError["data"];
    };
  };
  "/products/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameProduct>[1]>;
      bodyRequired: true;
      data: og.renameProductResponseSuccess["data"];
      error: og.renameProductResponseError["data"];
    };
  };
  "/property-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listPropertyTypeResponseSuccess["data"];
      error: og.listPropertyTypeResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createPropertyType>[0]>;
      bodyRequired: true;
      data: og.createPropertyTypeResponseSuccess["data"];
      error: og.createPropertyTypeResponseError["data"];
    };
  };
  "/property-types/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getPropertyTypeResponseSuccess["data"];
      error: og.getPropertyTypeResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deletePropertyTypeResponseSuccess["data"];
      error: og.deletePropertyTypeResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updatePropertyType>[1]>;
      bodyRequired: true;
      data: og.updatePropertyTypeResponseSuccess["data"];
      error: og.updatePropertyTypeResponseError["data"];
    };
  };
  "/property-types/{name}:setRetention": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setPropertyTypeRetention>[1]>;
      bodyRequired: true;
      data: og.setPropertyTypeRetentionResponseSuccess["data"];
      error: og.setPropertyTypeRetentionResponseError["data"];
    };
  };
  "/roles": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listRolesResponseSuccess["data"];
      error: og.listRolesResponseError["data"];
    };
  };
  "/secret-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSecretTypesResponseSuccess["data"];
      error: og.listSecretTypesResponseError["data"];
    };
  };
  "/secrets": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSecretsResponseSuccess["data"];
      error: og.listSecretsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createSecret>[0]>;
      bodyRequired: true;
      data: og.createSecretResponseSuccess["data"];
      error: og.createSecretResponseError["data"];
    };
  };
  "/secrets/{secret}": {
    delete: {
      path: { "secret": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSecretResponseSuccess["data"];
      error: og.deleteSecretResponseError["data"];
    };
    patch: {
      path: { "secret": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateSecret>[1]>;
      bodyRequired: true;
      data: og.updateSecretResponseSuccess["data"];
      error: og.updateSecretResponseError["data"];
    };
  };
  "/secrets/{secret}:copy": {
    post: {
      path: { "secret": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.copySecretResponseSuccess["data"];
      error: og.copySecretResponseError["data"];
    };
  };
  "/secrets/{secret}:reveal": {
    post: {
      path: { "secret": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.revealSecretResponseSuccess["data"];
      error: og.revealSecretResponseError["data"];
    };
  };
  "/settings": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSettingsResponseSuccess["data"];
      error: og.getSettingsResponseError["data"];
    };
  };
  "/settings/me": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSettingsMeResponseSuccess["data"];
      error: og.getSettingsMeResponseError["data"];
    };
  };
  "/settings/{namespace}": {
    delete: {
      path: { "namespace": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSettingsNamespaceResponseSuccess["data"];
      error: og.deleteSettingsNamespaceResponseError["data"];
    };
    patch: {
      path: { "namespace": string };
      query: never;
      body: NonNullable<Parameters<typeof og.patchSettingsNamespace>[1]>;
      bodyRequired: true;
      data: og.patchSettingsNamespaceResponseSuccess["data"];
      error: og.patchSettingsNamespaceResponseError["data"];
    };
  };
  "/settings:restoreDefaults": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.restoreSettingsDefaultsResponseSuccess["data"];
      error: og.restoreSettingsDefaultsResponseError["data"];
    };
  };
  "/severity-levels": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSeverityLevelResponseSuccess["data"];
      error: og.listSeverityLevelResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createSeverityLevel>[0]>;
      bodyRequired: true;
      data: og.createSeverityLevelResponseSuccess["data"];
      error: og.createSeverityLevelResponseError["data"];
    };
  };
  "/severity-levels/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSeverityLevelResponseSuccess["data"];
      error: og.getSeverityLevelResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSeverityLevelResponseSuccess["data"];
      error: og.deleteSeverityLevelResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateSeverityLevel>[1]>;
      bodyRequired: true;
      data: og.updateSeverityLevelResponseSuccess["data"];
      error: og.updateSeverityLevelResponseError["data"];
    };
  };
  "/simulation": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSimulationResponseSuccess["data"];
      error: og.getSimulationResponseError["data"];
    };
  };
  "/simulation/profiles": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSimulationProfilesResponseSuccess["data"];
      error: og.listSimulationProfilesResponseError["data"];
    };
  };
  "/simulation/standards/{standard}:plan": {
    get: {
      path: { "standard": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.planSimulationSystemResponseSuccess["data"];
      error: og.planSimulationSystemResponseError["data"];
    };
  };
  "/standards": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listStandardsResponseSuccess["data"];
      error: og.listStandardsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createStandard>[0]>;
      bodyRequired: true;
      data: og.createStandardResponseSuccess["data"];
      error: og.createStandardResponseError["data"];
    };
  };
  "/standards/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getStandardResponseSuccess["data"];
      error: og.getStandardResponseError["data"];
    };
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteStandardResponseSuccess["data"];
      error: og.deleteStandardResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateStandard>[1]>;
      bodyRequired: true;
      data: og.updateStandardResponseSuccess["data"];
      error: og.updateStandardResponseError["data"];
    };
  };
  "/standards/{id}/metrics": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listStandardMetricsResponseSuccess["data"];
      error: og.listStandardMetricsResponseError["data"];
    };
  };
  "/standards/{id}/metrics/{metric}": {
    put: {
      path: { "id": string; "metric": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setStandardMetric>[2]>;
      bodyRequired: true;
      data: og.setStandardMetricResponseSuccess["data"];
      error: og.setStandardMetricResponseError["data"];
    };
    delete: {
      path: { "id": string; "metric": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteStandardMetricResponseSuccess["data"];
      error: og.deleteStandardMetricResponseError["data"];
    };
  };
  "/standards/{id}/properties": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listStandardPropertiesResponseSuccess["data"];
      error: og.listStandardPropertiesResponseError["data"];
    };
  };
  "/standards/{id}/properties/{property}": {
    put: {
      path: { "id": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setStandardProperty>[2]>;
      bodyRequired: true;
      data: og.setStandardPropertyResponseSuccess["data"];
      error: og.setStandardPropertyResponseError["data"];
    };
    delete: {
      path: { "id": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteStandardPropertyResponseSuccess["data"];
      error: og.deleteStandardPropertyResponseError["data"];
    };
  };
  "/standards/{id}/roles": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listStandardRolesResponseSuccess["data"];
      error: og.listStandardRolesResponseError["data"];
    };
  };
  "/standards/{id}/roles/{role}": {
    delete: {
      path: { "id": string; "role": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteStandardRoleResponseSuccess["data"];
      error: og.deleteStandardRoleResponseError["data"];
    };
    patch: {
      path: { "id": string; "role": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setStandardRole>[2]>;
      bodyRequired: true;
      data: og.setStandardRoleResponseSuccess["data"];
      error: og.setStandardRoleResponseError["data"];
    };
  };
  "/standards/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameStandard>[1]>;
      bodyRequired: true;
      data: og.renameStandardResponseSuccess["data"];
      error: og.renameStandardResponseError["data"];
    };
  };
  "/standards:createFromTemplate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createStandardFromTemplate>[0]>;
      bodyRequired: true;
      data: og.createStandardFromTemplateResponseSuccess["data"];
      error: og.createStandardFromTemplateResponseError["data"];
    };
  };
  "/system-types": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemTypesResponseSuccess["data"];
      error: og.listSystemTypesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createSystemType>[0]>;
      bodyRequired: true;
      data: og.createSystemTypeResponseSuccess["data"];
      error: og.createSystemTypeResponseError["data"];
    };
  };
  "/system-types/{id}": {
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSystemTypeResponseSuccess["data"];
      error: og.deleteSystemTypeResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateSystemType>[1]>;
      bodyRequired: true;
      data: og.updateSystemTypeResponseSuccess["data"];
      error: og.updateSystemTypeResponseError["data"];
    };
  };
  "/systems": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemsResponseSuccess["data"];
      error: og.listSystemsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createSystem>[0]>;
      bodyRequired: true;
      data: og.createSystemResponseSuccess["data"];
      error: og.createSystemResponseError["data"];
    };
  };
  "/systems/{name}": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSystemResponseSuccess["data"];
      error: og.getSystemResponseError["data"];
    };
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSystemResponseSuccess["data"];
      error: og.deleteSystemResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateSystem>[1]>;
      bodyRequired: true;
      data: og.updateSystemResponseSuccess["data"];
      error: og.updateSystemResponseError["data"];
    };
  };
  "/systems/{name}/alarms": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemAlarmsResponseSuccess["data"];
      error: og.listSystemAlarmsResponseError["data"];
    };
  };
  "/systems/{name}/events": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemEventsResponseSuccess["data"];
      error: og.listSystemEventsResponseError["data"];
    };
  };
  "/systems/{name}/health": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getSystemHealthResponseSuccess["data"];
      error: og.getSystemHealthResponseError["data"];
    };
  };
  "/systems/{name}/logs": {
    get: {
      path: { "name": string };
      query: NonNullable<Parameters<typeof og.listSystemLogs>[1]>;
      body: never;
      bodyRequired: false;
      data: og.listSystemLogsResponseSuccess["data"];
      error: og.listSystemLogsResponseError["data"];
    };
  };
  "/systems/{name}/members": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemMembersResponseSuccess["data"];
      error: og.listSystemMembersResponseError["data"];
    };
  };
  "/systems/{name}/members/{component}": {
    put: {
      path: { "name": string; "component": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.addSystemMemberResponseSuccess["data"];
      error: og.addSystemMemberResponseError["data"];
    };
    delete: {
      path: { "name": string; "component": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.removeSystemMemberResponseSuccess["data"];
      error: og.removeSystemMemberResponseError["data"];
    };
  };
  "/systems/{name}/members/{component}:setPrimary": {
    post: {
      path: { "name": string; "component": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.setPrimaryMemberResponseSuccess["data"];
      error: og.setPrimaryMemberResponseError["data"];
    };
  };
  "/systems/{name}/metrics": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemMetricsResponseSuccess["data"];
      error: og.listSystemMetricsResponseError["data"];
    };
  };
  "/systems/{name}/metrics/{metric}/samples": {
    get: {
      path: { "name": string; "metric": string };
      query: NonNullable<Parameters<typeof og.listSystemMetricSeries>[2]>;
      body: never;
      bodyRequired: false;
      data: og.listSystemMetricSeriesResponseSuccess["data"];
      error: og.listSystemMetricSeriesResponseError["data"];
    };
  };
  "/systems/{name}/properties": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemPropertiesResponseSuccess["data"];
      error: og.listSystemPropertiesResponseError["data"];
    };
  };
  "/systems/{name}/properties/{property}": {
    put: {
      path: { "name": string; "property": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setSystemProperty>[2]>;
      bodyRequired: true;
      data: og.setSystemPropertyResponseSuccess["data"];
      error: og.setSystemPropertyResponseError["data"];
    };
    delete: {
      path: { "name": string; "property": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.clearSystemPropertyResponseSuccess["data"];
      error: og.clearSystemPropertyResponseError["data"];
    };
  };
  "/systems/{name}/properties/{property}/samples": {
    get: {
      path: { "name": string; "property": string };
      query: NonNullable<Parameters<typeof og.listSystemPropertySeries>[2]>;
      body: never;
      bodyRequired: false;
      data: og.listSystemPropertySeriesResponseSuccess["data"];
      error: og.listSystemPropertySeriesResponseError["data"];
    };
  };
  "/systems/{name}/roles": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemRolesResponseSuccess["data"];
      error: og.listSystemRolesResponseError["data"];
    };
  };
  "/systems/{name}/roles/{role}": {
    delete: {
      path: { "name": string; "role": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteSystemRoleResponseSuccess["data"];
      error: og.deleteSystemRoleResponseError["data"];
    };
    patch: {
      path: { "name": string; "role": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setSystemRole>[2]>;
      bodyRequired: true;
      data: og.setSystemRoleResponseSuccess["data"];
      error: og.setSystemRoleResponseError["data"];
    };
  };
  "/systems/{name}/roles/{role}/assignments/{component}": {
    put: {
      path: { "name": string; "role": string; "component": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.assignSystemRoleResponseSuccess["data"];
      error: og.assignSystemRoleResponseError["data"];
    };
    delete: {
      path: { "name": string; "role": string; "component": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.unassignSystemRoleResponseSuccess["data"];
      error: og.unassignSystemRoleResponseError["data"];
    };
  };
  "/systems/{name}/roles/{role}:swapPositions": {
    post: {
      path: { "name": string; "role": string };
      query: never;
      body: NonNullable<Parameters<typeof og.swapRolePositions>[2]>;
      bodyRequired: true;
      data: og.swapRolePositionsResponseSuccess["data"];
      error: og.swapRolePositionsResponseError["data"];
    };
  };
  "/systems/{name}:listTags": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemTagsResponseSuccess["data"];
      error: og.listSystemTagsResponseError["data"];
    };
  };
  "/systems/{name}:move": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.moveSystem>[1]>;
      bodyRequired: true;
      data: og.moveSystemResponseSuccess["data"];
      error: og.moveSystemResponseError["data"];
    };
  };
  "/systems/{name}:removeTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.removeSystemTag>[1]>;
      bodyRequired: true;
      data: og.removeSystemTagResponseSuccess["data"];
      error: og.removeSystemTagResponseError["data"];
    };
  };
  "/systems/{name}:rename": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameSystem>[1]>;
      bodyRequired: true;
      data: og.renameSystemResponseSuccess["data"];
      error: og.renameSystemResponseError["data"];
    };
  };
  "/systems/{name}:resetName": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.resetSystemNameResponseSuccess["data"];
      error: og.resetSystemNameResponseError["data"];
    };
  };
  "/systems/{name}:setTag": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setSystemTag>[1]>;
      bodyRequired: true;
      data: og.setSystemTagResponseSuccess["data"];
      error: og.setSystemTagResponseError["data"];
    };
  };
  "/systems:checkName": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.checkSystemName>[0]>;
      bodyRequired: true;
      data: og.checkSystemNameResponseSuccess["data"];
      error: og.checkSystemNameResponseError["data"];
    };
  };
  "/systems:createFromTemplate": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createSystemFromTemplate>[0]>;
      bodyRequired: true;
      data: og.createSystemFromTemplateResponseSuccess["data"];
      error: og.createSystemFromTemplateResponseError["data"];
    };
  };
  "/systems:health": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listSystemVerdictsResponseSuccess["data"];
      error: og.listSystemVerdictsResponseError["data"];
    };
  };
  "/systems:previewLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.previewSystemLabelsResponseSuccess["data"];
      error: og.previewSystemLabelsResponseError["data"];
    };
  };
  "/systems:recomputeLabels": {
    post: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.recomputeSystemLabelsResponseSuccess["data"];
      error: og.recomputeSystemLabelsResponseError["data"];
    };
  };
  "/systems:renderLabel": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.renderSystemLabel>[0]>;
      bodyRequired: true;
      data: og.renderSystemLabelResponseSuccess["data"];
      error: og.renderSystemLabelResponseError["data"];
    };
  };
  "/tags": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listTagsResponseSuccess["data"];
      error: og.listTagsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createTag>[0]>;
      bodyRequired: true;
      data: og.createTagResponseSuccess["data"];
      error: og.createTagResponseError["data"];
    };
  };
  "/tags/{name}": {
    delete: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteTagResponseSuccess["data"];
      error: og.deleteTagResponseError["data"];
    };
    patch: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateTag>[1]>;
      bodyRequired: true;
      data: og.updateTagResponseSuccess["data"];
      error: og.updateTagResponseError["data"];
    };
  };
  "/tags/{name}:clearPlatform": {
    post: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.clearPlatformTagResponseSuccess["data"];
      error: og.clearPlatformTagResponseError["data"];
    };
  };
  "/tags/{name}:setPlatform": {
    post: {
      path: { "name": string };
      query: never;
      body: NonNullable<Parameters<typeof og.setPlatformTag>[1]>;
      bodyRequired: true;
      data: og.setPlatformTagResponseSuccess["data"];
      error: og.setPlatformTagResponseError["data"];
    };
  };
  "/tags/{name}:values": {
    get: {
      path: { "name": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listTagValuesResponseSuccess["data"];
      error: og.listTagValuesResponseError["data"];
    };
  };
  "/tasks": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listTasksResponseSuccess["data"];
      error: og.listTasksResponseError["data"];
    };
  };
  "/tasks/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getTaskResponseSuccess["data"];
      error: og.getTaskResponseError["data"];
    };
  };
  "/telemetry:push": {
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.pushTelemetry>[0]>;
      bodyRequired: true;
      data: og.pushTelemetryResponseSuccess["data"];
      error: og.pushTelemetryResponseError["data"];
    };
  };
  "/templates": {
    get: {
      path: never;
      query: NonNullable<Parameters<typeof og.listTemplates>[0]>;
      body: never;
      bodyRequired: false;
      data: og.listTemplatesResponseSuccess["data"];
      error: og.listTemplatesResponseError["data"];
    };
  };
  "/templates/{template}": {
    get: {
      path: { "template": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getTemplateResponseSuccess["data"];
      error: og.getTemplateResponseError["data"];
    };
  };
  "/transports": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listTransportsResponseSuccess["data"];
      error: og.listTransportsResponseError["data"];
    };
  };
  "/unclaimed-sources": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listUnclaimedSourcesResponseSuccess["data"];
      error: og.listUnclaimedSourcesResponseError["data"];
    };
  };
  "/variables": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listVariablesResponseSuccess["data"];
      error: og.listVariablesResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createVariable>[0]>;
      bodyRequired: true;
      data: og.createVariableResponseSuccess["data"];
      error: og.createVariableResponseError["data"];
    };
  };
  "/variables/{variable}": {
    delete: {
      path: { "variable": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteVariableResponseSuccess["data"];
      error: og.deleteVariableResponseError["data"];
    };
    patch: {
      path: { "variable": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateVariable>[1]>;
      bodyRequired: true;
      data: og.updateVariableResponseSuccess["data"];
      error: og.updateVariableResponseError["data"];
    };
  };
  "/vendors": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.listVendorsResponseSuccess["data"];
      error: og.listVendorsResponseError["data"];
    };
    post: {
      path: never;
      query: never;
      body: NonNullable<Parameters<typeof og.createVendor>[0]>;
      bodyRequired: true;
      data: og.createVendorResponseSuccess["data"];
      error: og.createVendorResponseError["data"];
    };
  };
  "/vendors/{id}": {
    get: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getVendorResponseSuccess["data"];
      error: og.getVendorResponseError["data"];
    };
    delete: {
      path: { "id": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.deleteVendorResponseSuccess["data"];
      error: og.deleteVendorResponseError["data"];
    };
    patch: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.updateVendor>[1]>;
      bodyRequired: true;
      data: og.updateVendorResponseSuccess["data"];
      error: og.updateVendorResponseError["data"];
    };
  };
  "/vendors/{id}:rename": {
    post: {
      path: { "id": string };
      query: never;
      body: NonNullable<Parameters<typeof og.renameVendor>[1]>;
      bodyRequired: true;
      data: og.renameVendorResponseSuccess["data"];
      error: og.renameVendorResponseError["data"];
    };
  };
  "/views/fleet": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getFleetViewResponseSuccess["data"];
      error: og.getFleetViewResponseError["data"];
    };
  };
  "/views/platform": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getPlatformViewResponseSuccess["data"];
      error: og.getPlatformViewResponseError["data"];
    };
  };
  "/views/platform/simulation": {
    get: {
      path: never;
      query: never;
      body: never;
      bodyRequired: false;
      data: og.getPlatformSimulationViewResponseSuccess["data"];
      error: og.getPlatformSimulationViewResponseError["data"];
    };
  };
  "/webhooks/{token}": {
    get: {
      path: { "token": string };
      query: never;
      body: never;
      bodyRequired: false;
      data: og.verifyWebhookResponseSuccess["data"];
      error: og.verifyWebhookResponseError["data"];
    };
    post: {
      path: { "token": string };
      query: never;
      body: NonNullable<Parameters<typeof og.receiveWebhook>[1]>;
      bodyRequired: true;
      data: og.receiveWebhookResponseSuccess["data"];
      error: og.receiveWebhookResponseError["data"];
    };
  };
}

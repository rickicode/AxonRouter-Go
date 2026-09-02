// English source-of-truth dictionary for AxonRouter-Go.
// Every key is a dotted path describing the UI slot. Values are the exact English wording that
// currently appears in the code — nothing visibly changes when the locale is "en".
//
// A locale JSON dictionary overrides individual keys. A missing key (in either the JSON file or
// the look-up) falls back to the value below. A locale with no file renders English.
//
// HOW TO ADD KEYS:
//   1. Add the English value here (under the right namespace).
//   2. TypeScript infers the key union, so $t('foo.bar') is compile-checked.
//   3. A locale JSON file only needs to list the keys it overrides.

export const en = {
  common: {
    cancel: 'Cancel',
    save: 'Save',
    delete: 'Delete',
    clear: 'Clear',
    refresh: 'Refresh',
    no_data: 'No data available.',
  },
  nav: {
    section: { platform: 'Platform', system: 'System' },
    dashboard: 'Dashboard',
    providers: 'Providers',
    combos: 'Combos',
    usage: 'Usage',
    quota: 'Quota',
    optimization: 'Optimization',
    logs: 'Logs',
    console: 'Console',
    translatorDebug: 'Translator Debug',
    proxyPools: 'Proxy Pools',
    proxyFitness: 'Proxy Fitness',
    apiKeys: 'API Keys',
    developers: 'Developers',
    cliTools: 'CLI Tools',
    modelPricing: 'Model Pricing',
    mcp: 'MCP',
    backupRestore: 'Backup & Restore',
    settings: 'Settings',
    about: 'About',
  },
  app: {
    supportUs: 'Support us',
    logout: 'Logout',
    signedOut: 'Signed out',
  },
  login: {
    title: 'Sign in.',
    subtitle: 'Enter your admin password to access the dashboard.',
    passwordLabel: 'Password',
    passwordPlaceholder: 'Enter your password',
    showPasswordLabel: 'Show password',
    hidePasswordLabel: 'Hide password',
    signIn: 'Sign in',
    signingIn: 'Signing in…',
    hint: 'The initial admin password is {code}. Change it from Settings or via the CLI.',
    footer: 'AxonRouter Dashboard',
    failed: 'Login failed',
  },
  dashboard: {
    title: 'Dashboard.',
    subtitle: 'Auto-refreshing overview of traffic, cost, and system health.',
    tryAgain: 'Try again',
    kpi: {
      requests: 'Requests',
      tokens: 'Tokens',
      cost: 'Cost',
      errors: 'Errors',
      avgLatency: 'Avg latency',
      cpu: 'CPU',
      memory: 'Memory',
      disk: 'Disk',
      connections: 'Connections',
      providers: 'Providers',
      combos: 'Combos',
      uptime: 'Uptime',
      activeRequests: 'Active requests',
    },
    kpiSub: {
      today: 'today',
      cores: '{n} cores',
      registered: 'registered',
      configured: 'configured',
      sinceStart: 'since start',
      live: 'live',
    },
    connectionAllHealthy: 'all healthy',
    connectionError: 'error',
    connectionErrors: 'errors',
    connectionStatus: 'Connection status',
    noConnectionData: 'No connection status data.',
    budgetTitle: 'API key budget utilization',
    noBudgetKeys: 'No API keys with budget limits configured.',
    budgetLegend: { below: 'Below threshold', atAbove: 'At/above threshold', over: 'Over limit' },
    budgetDaily: 'Daily',
    budgetMonthly: 'Monthly',
    budgetThreshold: 'Threshold: {percent}%',
    failed: 'Failed to load dashboard',
  },
  providers: {
    title: 'Providers',
    subtitle: 'OmniRoute-style provider catalog with AxonRouter connection health, auth labels, prefixes, and model surface details.',
    syncModels: 'Sync models',
    syncing: 'Syncing…',
    addProvider: 'Add provider',
    stats: {
      catalog: 'Catalog',
      connections: 'Connections',
      ready: 'Ready',
      needsAttention: 'Needs attention',
      configured: '{n} configured',
      runtimePool: 'runtime pool',
      availableRoutes: 'available routes',
      quotaAuthCooldown: 'quota, auth, cooldown',
    },
    searchPlaceholder: 'Search providers…',
    clearSearch: 'Clear search',
    shownCount: '{n} shown',
    sectionsCount: '{n} sections',
    filterAll: 'All',
    freeTier: 'Free tier providers',
    free: 'Free',
    categoryProviders: '{category} providers',
    readyCount: '{n} ready',
    issuesCount: '{n} issues',
    disabledCount: '{n} disabled',
    connectionsCount: '{n} connections',
    errorTryAgain: 'Try again',
    noMatch: 'No providers match your filters.',
    resetFilters: 'Reset filters',
    syncSuccess: 'Models synced successfully',
    syncFailed: 'Sync failed: {message}',
  },
  settings: {
    title: 'Settings.',
    subtitle: 'Manage runtime parameters, security, HTTPS, and smart routing.',
    tabs: { runtime: 'Runtime', security: 'Security', https: 'HTTPS', smartRouter: 'Smart Router' },
    dataManagement: 'Data Management',
    dataManagementDesc: 'Export, import, and migrate settings data.',
    exportButton: 'Export settings (JSON)',
    importButton: 'Import settings',
    cancelImport: 'Cancel import',
    importLabel: 'Paste settings JSON',
    importPlaceholder: 'Paste JSON settings object here…',
    importAction: 'Import',
    cancel: 'Cancel',
    imported: 'Settings imported',
    importFailed: 'Import failed: {message}',
  },
  changePassword: {
    title: 'Change Password',
    subtitle: 'Update the admin dashboard password.',
    currentLabel: 'Current Password',
    currentPlaceholder: 'Enter current password',
    newLabel: 'New Password',
    newPlaceholder: 'Enter new password (min. 8 characters)',
    confirmLabel: 'Confirm New Password',
    confirmPlaceholder: 'Repeat new password',
    showPassword: 'Show password',
    hidePassword: 'Hide password',
    minLengthHint: 'Use at least 8 characters.',
    saving: 'Saving…',
    updateButton: 'Update Password',
    requiredCurrent: 'Current password is required',
    minLengthNew: 'New password must be at least 8 characters',
    mismatch: 'New passwords do not match',
    updated: 'Password updated',
    failed: 'Failed to update password',
  },
  language: {
    untranslatedHint: 'This locale is not yet translated; English content is shown.',
  },
} as const;

export type Messages = typeof en;
// Dotted-path union of every leaf string in Messages.
export type TranslationKey =
  | 'common.cancel'
  | 'common.save'
  | 'common.delete'
  | 'common.clear'
  | 'common.refresh'
  | 'common.no_data'
  | 'nav.section.platform'
  | 'nav.section.system'
  | 'nav.dashboard'
  | 'nav.providers'
  | 'nav.combos'
  | 'nav.usage'
  | 'nav.quota'
  | 'nav.optimization'
  | 'nav.logs'
  | 'nav.console'
  | 'nav.translatorDebug'
  | 'nav.proxyPools'
  | 'nav.proxyFitness'
  | 'nav.apiKeys'
  | 'nav.developers'
  | 'nav.cliTools'
  | 'nav.modelPricing'
  | 'nav.mcp'
  | 'nav.backupRestore'
  | 'nav.settings'
  | 'nav.about'
  | 'app.supportUs'
  | 'app.logout'
  | 'app.signedOut'
  | 'login.title'
  | 'login.subtitle'
  | 'login.passwordLabel'
  | 'login.passwordPlaceholder'
  | 'login.showPasswordLabel'
  | 'login.hidePasswordLabel'
  | 'login.signIn'
  | 'login.signingIn'
  | 'login.hint'
  | 'login.footer'
  | 'login.failed'
  | 'dashboard.title'
  | 'dashboard.subtitle'
  | 'dashboard.tryAgain'
  | 'dashboard.kpi.requests'
  | 'dashboard.kpi.tokens'
  | 'dashboard.kpi.cost'
  | 'dashboard.kpi.errors'
  | 'dashboard.kpi.avgLatency'
  | 'dashboard.kpi.cpu'
  | 'dashboard.kpi.memory'
  | 'dashboard.kpi.disk'
  | 'dashboard.kpi.connections'
  | 'dashboard.kpi.providers'
  | 'dashboard.kpi.combos'
  | 'dashboard.kpi.uptime'
  | 'dashboard.kpi.activeRequests'
  | 'dashboard.kpiSub.today'
  | 'dashboard.kpiSub.cores'
  | 'dashboard.kpiSub.registered'
  | 'dashboard.kpiSub.configured'
  | 'dashboard.kpiSub.sinceStart'
  | 'dashboard.kpiSub.live'
  | 'dashboard.connectionAllHealthy'
  | 'dashboard.connectionError'
  | 'dashboard.connectionErrors'
  | 'dashboard.connectionStatus'
  | 'dashboard.noConnectionData'
  | 'dashboard.budgetTitle'
  | 'dashboard.noBudgetKeys'
  | 'dashboard.budgetLegend.below'
  | 'dashboard.budgetLegend.atAbove'
  | 'dashboard.budgetLegend.over'
  | 'dashboard.budgetDaily'
  | 'dashboard.budgetMonthly'
  | 'dashboard.budgetThreshold'
  | 'dashboard.failed'
  | 'providers.title'
  | 'providers.subtitle'
  | 'providers.syncModels'
  | 'providers.syncing'
  | 'providers.addProvider'
  | 'providers.stats.catalog'
  | 'providers.stats.connections'
  | 'providers.stats.ready'
  | 'providers.stats.needsAttention'
  | 'providers.stats.configured'
  | 'providers.stats.runtimePool'
  | 'providers.stats.availableRoutes'
  | 'providers.stats.quotaAuthCooldown'
  | 'providers.searchPlaceholder'
  | 'providers.clearSearch'
  | 'providers.shownCount'
  | 'providers.sectionsCount'
  | 'providers.filterAll'
  | 'providers.freeTier'
  | 'providers.free'
  | 'providers.categoryProviders'
  | 'providers.readyCount'
  | 'providers.issuesCount'
  | 'providers.disabledCount'
  | 'providers.connectionsCount'
  | 'providers.errorTryAgain'
  | 'providers.noMatch'
  | 'providers.resetFilters'
  | 'providers.syncSuccess'
  | 'providers.syncFailed'
  | 'settings.title'
  | 'settings.subtitle'
  | 'settings.tabs.runtime'
  | 'settings.tabs.security'
  | 'settings.tabs.https'
  | 'settings.tabs.smartRouter'
  | 'settings.dataManagement'
  | 'settings.dataManagementDesc'
  | 'settings.exportButton'
  | 'settings.importButton'
  | 'settings.cancelImport'
  | 'settings.importLabel'
  | 'settings.importPlaceholder'
  | 'settings.importAction'
  | 'settings.cancel'
  | 'settings.imported'
  | 'settings.importFailed'
  | 'changePassword.title'
  | 'changePassword.subtitle'
  | 'changePassword.currentLabel'
  | 'changePassword.currentPlaceholder'
  | 'changePassword.newLabel'
  | 'changePassword.newPlaceholder'
  | 'changePassword.confirmLabel'
  | 'changePassword.confirmPlaceholder'
  | 'changePassword.showPassword'
  | 'changePassword.hidePassword'
  | 'changePassword.minLengthHint'
  | 'changePassword.saving'
  | 'changePassword.updateButton'
  | 'changePassword.requiredCurrent'
  | 'changePassword.minLengthNew'
  | 'changePassword.mismatch'
  | 'changePassword.updated'
  | 'changePassword.failed'
  | 'language.untranslatedHint';
/// <reference types="vite/client" />

export {}

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresUserAuth?: boolean
    adminOnly?: boolean
    titleKey?: string
    primaryNav?: string
    publicLayout?: 'landing' | 'clientarea'
  }
}

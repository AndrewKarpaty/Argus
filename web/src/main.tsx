import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'
import { RouterProvider } from './lib/router'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // An incident console is read constantly and refetching is cheap, but a
      // failed request should not retry forever behind the user's back.
      retry: 1,
      refetchOnWindowFocus: true,
      staleTime: 2_000,
    },
  },
})

const container = document.getElementById('root')
if (!container) throw new Error('#root is missing from index.html')

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider>
        <App />
      </RouterProvider>
    </QueryClientProvider>
  </StrictMode>,
)

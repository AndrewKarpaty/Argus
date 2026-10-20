import { Layout } from './components/Layout'
import { Empty } from './components/primitives'
import { match, useRouter } from './lib/router'
import { IncidentsPage } from './pages/IncidentsPage'
import { InvestigationPage } from './pages/InvestigationPage'
import { TopologyPage } from './pages/TopologyPage'

export function App() {
  const { path } = useRouter()

  const investigationId = match(path, '/investigations')

  let page: React.ReactNode
  if (investigationId) {
    page = <InvestigationPage id={investigationId} />
  } else if (path === '/topology') {
    page = <TopologyPage />
  } else if (path === '/' || path === '/incidents') {
    page = <IncidentsPage />
  } else {
    page = <Empty title="Nothing here">That address does not match a page.</Empty>
  }

  return <Layout>{page}</Layout>
}

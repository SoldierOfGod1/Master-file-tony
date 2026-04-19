import { Outlet } from 'react-router-dom'
import { CommandCentreProvider, useCommandCentre } from './context/CommandCentreContext'
import Sidebar from './components/layout/Sidebar'
import Header from './components/layout/Header'
import { useClock } from './hooks/useClock'
import styles from './App.module.css'

function AppLayout() {
  const { state } = useCommandCentre()
  const clock = useClock()

  return (
    <div className={styles.layout}>
      <Sidebar />
      <div className={styles.main}>
        <Header clock={clock} gatewayStatus={state.gatewayStatus} />
        <div className={styles.content}>
          <Outlet />
        </div>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <CommandCentreProvider>
      <AppLayout />
    </CommandCentreProvider>
  )
}

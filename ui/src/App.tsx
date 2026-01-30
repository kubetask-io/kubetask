import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import TasksPage from './pages/TasksPage';
import TaskDetailPage from './pages/TaskDetailPage';
import TaskCreatePage from './pages/TaskCreatePage';
import AgentsPage from './pages/AgentsPage';
import AgentDetailPage from './pages/AgentDetailPage';

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/tasks" replace />} />
          <Route path="tasks" element={<TasksPage />} />
          <Route path="tasks/create" element={<TaskCreatePage />} />
          <Route path="tasks/:namespace/:name" element={<TaskDetailPage />} />
          <Route path="agents" element={<AgentsPage />} />
          <Route path="agents/:namespace/:name" element={<AgentDetailPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

export default App;

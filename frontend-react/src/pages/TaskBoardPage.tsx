/* ============================================================
   TaskBoardPage — 4-column kanban built from HudPanels.
   Drag-and-drop retained; panel columns replace the old
   glass cards.
   ============================================================ */

import { useState, useCallback, type DragEvent } from 'react';
import { Kanban, Clock, User } from 'lucide-react';
import { useCommandCentre } from '../context/CommandCentreContext';
import { updateTask } from '../api/tasks';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import type { Task } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './TaskBoardPage.module.css';

const COLUMNS = ['Inbox', 'In Progress', 'Review', 'Done'] as const;
type ColumnName = (typeof COLUMNS)[number];

/* One accent colour per column so the panel borders differ by state. */
const COLUMN_COLOR: Record<ColumnName, string> = {
  'Inbox':       '#7cc6ff',
  'In Progress': '#00f0ff',
  'Review':      '#ffc566',
  'Done':        '#6ff2a0',
};

/* Priority → colour mapping. Matches the ClickUp priority palette. */
function priorityColor(priority: string): string {
  const p = priority.toUpperCase();
  if (p === 'P1') return '#ff3355';
  if (p === 'P2') return '#ffaa00';
  return '#7cc6ff';
}

function TaskCard({
  task,
  onDragStart,
}: {
  readonly task: Task;
  readonly onDragStart: (e: DragEvent<HTMLDivElement>, taskId: string) => void;
}) {
  return (
    <div
      className={styles.taskCard}
      draggable
      onDragStart={(e) => onDragStart(e, task.id)}
    >
      <div className={styles.taskTitle}>{task.title}</div>
      <div className={styles.taskMeta}>
        <HudChip color="#00f0ff">
          <User size={8} style={{ marginRight: 3 }} />
          {task.agent}
        </HudChip>
        <HudChip color={priorityColor(task.priority)}>{task.priority}</HudChip>
        <span className={styles.taskTime}>
          <Clock size={9} /> {task.time}
        </span>
      </div>
    </div>
  );
}

function TaskColumn({
  name,
  tasks,
  onDragStart,
  onDrop,
}: {
  readonly name: ColumnName;
  readonly tasks: readonly Task[];
  readonly onDragStart: (e: DragEvent<HTMLDivElement>, taskId: string) => void;
  readonly onDrop: (e: DragEvent<HTMLDivElement>, column: ColumnName) => void;
}) {
  const [dragOver, setDragOver] = useState(false);
  const accent = COLUMN_COLOR[name];

  return (
    <div
      className={`${styles.column} ${dragOver ? styles.columnDragOver : ''}`}
      onDragOver={(e) => {
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        onDrop(e, name);
      }}
    >
      <HudPanel
        title={name}
        accent={accent}
        leading={<HudStatusLed color={accent} animate={tasks.length > 0} />}
        meta={<>{tasks.length}</>}
      >
        <div className={styles.taskStack}>
          {tasks.length === 0 ? (
            <div className={styles.dropHint}>// drop tasks here</div>
          ) : (
            tasks.map((t) => <TaskCard key={t.id} task={t} onDragStart={onDragStart} />)
          )}
        </div>
      </HudPanel>
    </div>
  );
}

export default function TaskBoardPage() {
  const { state, dispatch } = useCommandCentre();

  const handleDragStart = useCallback(
    (e: DragEvent<HTMLDivElement>, taskId: string) => {
      e.dataTransfer.setData('text/plain', taskId);
      e.dataTransfer.effectAllowed = 'move';
    },
    [],
  );

  const handleDrop = useCallback(
    async (e: DragEvent<HTMLDivElement>, newColumn: ColumnName) => {
      const taskId = e.dataTransfer.getData('text/plain');
      if (!taskId) return;
      const updated = state.tasks.map((t) =>
        t.id === taskId ? { ...t, column: newColumn } : t,
      );
      dispatch({ type: 'SET_TASKS', payload: updated });
      try {
        await updateTask(taskId, { column: newColumn });
      } catch {
        dispatch({ type: 'SET_TASKS', payload: state.tasks });
      }
    },
    [state.tasks, dispatch],
  );

  const tasksByColumn = COLUMNS.reduce<Record<ColumnName, Task[]>>(
    (acc, col) => ({
      ...acc,
      [col]: state.tasks.filter((t) => t.column === col),
    }),
    {} as Record<ColumnName, Task[]>,
  );

  const done = tasksByColumn['Done'].length;
  const total = state.tasks.length;
  const ratio = total === 0 ? 0 : done / total;

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Task Board · Kanban"
        subtitle={`${total} tasks · ${done} complete · ${total - done} outstanding`}
        gaugeValue={ratio}
        gaugeReadout={`${done}/${total}`}
        gaugeLabel="DONE"
        gaugeColor="#6ff2a0"
        segments={COLUMNS.map((col) => ({
          label: col,
          value: tasksByColumn[col].length,
          color: COLUMN_COLOR[col],
        }))}
        extra={
          <div className={styles.legendIcon}>
            <Kanban size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
      />

      <div className={styles.board}>
        {COLUMNS.map((col) => (
          <TaskColumn
            key={col}
            name={col}
            tasks={tasksByColumn[col]}
            onDragStart={handleDragStart}
            onDrop={handleDrop}
          />
        ))}
      </div>
    </div>
  );
}

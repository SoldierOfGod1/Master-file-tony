import styles from './FilterButtons.module.css';

interface FilterButtonsProps {
  readonly filters: readonly string[];
  readonly activeFilter: string;
  readonly onFilterChange: (filter: string) => void;
}

export default function FilterButtons({
  filters,
  activeFilter,
  onFilterChange,
}: FilterButtonsProps) {
  return (
    <div className={styles.row}>
      {filters.map((filter) => (
        <button
          key={filter}
          type="button"
          className={`${styles.btn} ${
            filter === activeFilter ? styles.btnActive : ''
          }`}
          onClick={() => onFilterChange(filter)}
        >
          {filter}
        </button>
      ))}
    </div>
  );
}

interface IconPickerProps {
  value: string;
  onChange: (icon: string) => void;
}

const PRESET_ICONS = [
  // Business
  '👤', '👥', '🏢', '📦', '💰', '📊', '📋', '🔧',
  // Objects
  '📁', '📄', '🔗', '⚡', '🌐', '🗓️', '📌', '🏷️',
  // Status
  '✅', '❌', '⚠️', '🔄', '💡', '🎯', '🔒', '🔑',
  // Other
  '🚀', '📱', '💻', '🛒', '✈️', '🏥', '🎓', '🏭',
];

export function IconPicker({ value, onChange }: IconPickerProps) {
  return (
    <div className="space-y-2">
      <div className="grid grid-cols-8 gap-1">
        {PRESET_ICONS.map((icon) => {
          const isSelected = value === icon;
          return (
            <button
              key={icon}
              type="button"
              aria-label={icon}
              onClick={() => onChange(icon)}
              className={`w-8 h-8 flex items-center justify-center rounded text-base transition-all focus:outline-none focus:ring-1 focus:ring-accent-cyan border ${
                isSelected
                  ? 'border-accent-cyan bg-accent-cyan/10'
                  : 'border-transparent hover:border-border hover:bg-bg-tertiary'
              }`}
            >
              {icon}
            </button>
          );
        })}
      </div>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Custom emoji or name"
        className="w-full bg-bg-tertiary border border-border rounded px-2 py-1 text-xs text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan"
      />
    </div>
  );
}

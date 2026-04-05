interface ColorPickerProps {
  value: string;
  onChange: (color: string) => void;
}

const PRESET_COLORS = [
  { label: 'Red', hex: '#ef4444' },
  { label: 'Orange', hex: '#f97316' },
  { label: 'Yellow', hex: '#eab308' },
  { label: 'Green', hex: '#22c55e' },
  { label: 'Blue', hex: '#3b82f6' },
  { label: 'Purple', hex: '#a855f7' },
  { label: 'Pink', hex: '#ec4899' },
  { label: 'Cyan', hex: '#06b6d4' },
  { label: 'Teal', hex: '#14b8a6' },
  { label: 'Indigo', hex: '#6366f1' },
  { label: 'Gray', hex: '#6b7280' },
  { label: 'Slate', hex: '#475569' },
];

export function ColorPicker({ value, onChange }: ColorPickerProps) {
  return (
    <div className="space-y-2">
      <div className="grid grid-cols-6 gap-1.5">
        {PRESET_COLORS.map((color) => {
          const isSelected = value.toLowerCase() === color.hex.toLowerCase();
          return (
            <button
              key={color.hex}
              type="button"
              aria-label={color.label}
              title={color.label}
              onClick={() => onChange(color.hex)}
              className={`w-6 h-6 rounded-full transition-all focus:outline-none focus:ring-2 focus:ring-accent-cyan focus:ring-offset-1 focus:ring-offset-bg-tertiary ${
                isSelected ? 'ring-2 ring-white ring-offset-1 ring-offset-bg-tertiary scale-110' : 'hover:scale-110'
              }`}
              style={{ backgroundColor: color.hex }}
            />
          );
        })}
      </div>
      <div className="flex items-center gap-2">
        <div
          className="w-6 h-6 rounded flex-shrink-0 border border-border"
          style={{ backgroundColor: value || '#6b7280' }}
        />
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="#000000"
          className="flex-1 bg-bg-tertiary border border-border rounded px-2 py-1 text-xs font-mono text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan"
        />
      </div>
    </div>
  );
}

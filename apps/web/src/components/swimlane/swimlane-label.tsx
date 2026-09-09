"use client";

interface SwimlaneLabelProps {
  label: string;
  lastActive?: Date;
  color: string;
  compact?: boolean;
}

export function SwimlaneLabel({
  label,
  lastActive,
  color,
  compact = false,
}: SwimlaneLabelProps) {
  return (
    <div
      className={`flex w-full items-center ${compact ? "gap-1.5 px-1 pb-1" : "gap-2 px-1 pb-3"}`}
    >
      <span aria-hidden="true" className="size-2 rounded-full" style={{ backgroundColor: color }} />
      <span className={`${compact ? "text-[11px]" : "text-xs"} font-semibold text-foreground/80`}>
        {label}
      </span>
      {lastActive && (
        <span className={`${compact ? "text-[10px]" : "text-xs"} text-muted-foreground`}>
          active {formatLastActive(lastActive)}
        </span>
      )}
    </div>
  );
}

function swimlaneHue(name: string): number {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = name.charCodeAt(i) + ((hash << 5) - hash);
  }
  return ((hash % 360) + 360) % 360;
}

/**
 * Generate a soft pastel background color from a string.
 * Returns a CSS `light-dark()` value that adapts to the color scheme.
 */
export function swimlaneColor(name: string): string {
  const hue = swimlaneHue(name);
  return `light-dark(hsl(${hue} 16% 95%), hsl(${hue} 10% 17%))`;
}

/** Saturated accent color for borders/highlights derived from the swimlane hue. */
export function swimlaneAccent(name: string): string {
  const hue = swimlaneHue(name);
  return `light-dark(hsl(${hue} 50% 65%), hsl(${hue} 40% 45%))`;
}

function formatLastActive(date: Date): string {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
  }).format(date);
}

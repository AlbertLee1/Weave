const SPARK_W = 120;
const SPARK_H = 24;
const SPARK_PAD = 2;

export function VertexMiniSparkline({
  values,
  testId,
  className,
  stroke = '#3B82F6',
}: {
  values: number[];
  testId: string;
  className?: string;
  stroke?: string;
}) {
  const max = Math.max(...values);
  const min = Math.min(...values);
  const span = max - min || 1;
  const innerW = SPARK_W - SPARK_PAD * 2;
  const innerH = SPARK_H - SPARK_PAD * 2;
  const stepX = values.length > 1 ? innerW / (values.length - 1) : 0;
  const path = values
    .map((v, i) => {
      const x = SPARK_PAD + i * stepX;
      const y = SPARK_PAD + innerH - ((v - min) / span) * innerH;
      return `${i === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`;
    })
    .join(' ');
  return (
    <svg
      data-testid={testId}
      viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
      preserveAspectRatio="none"
      className={className}
    >
      <path d={path} stroke={stroke} strokeWidth={1.5} fill="none" />
    </svg>
  );
}

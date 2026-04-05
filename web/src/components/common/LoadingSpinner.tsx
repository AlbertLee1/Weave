interface LoadingSpinnerProps {
  size?: 'sm' | 'md' | 'lg';
}

const sizeMap = {
  sm: { outer: 16, inner: 10, glow: 12 },
  md: { outer: 24, inner: 15, glow: 18 },
  lg: { outer: 36, inner: 22, glow: 28 },
};

export function LoadingSpinner({ size = 'md' }: LoadingSpinnerProps) {
  const { outer, inner, glow } = sizeMap[size];

  return (
    <div
      className="flex items-center justify-center"
      data-testid="loading-spinner"
      style={{ width: outer, height: outer, position: 'relative' }}
    >
      {/* Pulse glow behind both rings */}
      <span
        style={{
          position: 'absolute',
          width: glow,
          height: glow,
          borderRadius: '50%',
          background: 'radial-gradient(circle, rgba(245,158,11,0.18) 0%, transparent 70%)',
          animation: 'spinnerPulse 2s ease-in-out infinite',
        }}
      />

      {/* Outer ring — amber, clockwise */}
      <svg
        width={outer}
        height={outer}
        viewBox={`0 0 ${outer} ${outer}`}
        fill="none"
        style={{
          position: 'absolute',
          animation: 'spin 1.1s linear infinite',
        }}
      >
        <circle
          cx={outer / 2}
          cy={outer / 2}
          r={outer / 2 - 2}
          stroke="rgba(245,158,11,0.15)"
          strokeWidth="2"
        />
        <circle
          cx={outer / 2}
          cy={outer / 2}
          r={outer / 2 - 2}
          stroke="#F59E0B"
          strokeWidth="2"
          strokeLinecap="round"
          strokeDasharray={`${Math.PI * (outer - 4) * 0.3} ${Math.PI * (outer - 4) * 0.7}`}
          style={{ filter: 'drop-shadow(0 0 3px rgba(245,158,11,0.6))' }}
        />
      </svg>

      {/* Inner ring — teal, counter-clockwise */}
      <svg
        width={inner}
        height={inner}
        viewBox={`0 0 ${inner} ${inner}`}
        fill="none"
        style={{
          position: 'absolute',
          animation: 'spinReverse 0.8s linear infinite',
        }}
      >
        <circle
          cx={inner / 2}
          cy={inner / 2}
          r={inner / 2 - 1.5}
          stroke="rgba(20,184,166,0.15)"
          strokeWidth="1.5"
        />
        <circle
          cx={inner / 2}
          cy={inner / 2}
          r={inner / 2 - 1.5}
          stroke="#14B8A6"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeDasharray={`${Math.PI * (inner - 3) * 0.4} ${Math.PI * (inner - 3) * 0.6}`}
          style={{ filter: 'drop-shadow(0 0 2px rgba(20,184,166,0.6))' }}
        />
      </svg>

      <style>{`
        @keyframes spin         { to { transform: rotate(360deg);  } }
        @keyframes spinReverse  { to { transform: rotate(-360deg); } }
        @keyframes spinnerPulse {
          0%, 100% { opacity: 0.5; transform: scale(0.9); }
          50%       { opacity: 1;   transform: scale(1.1); }
        }
      `}</style>
    </div>
  );
}

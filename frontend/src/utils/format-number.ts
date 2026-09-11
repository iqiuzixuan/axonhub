export function formatNumber(value: number | null | undefined, options?: { digits?: number; trimTrailingZeros?: boolean }) {
  if (value == null || Number.isNaN(value)) {
    return '0';
  }

  const digits = options?.digits ?? 1;
  const trimTrailingZeros = options?.trimTrailingZeros ?? true;
  const absolute = Math.abs(value);

  const formatWithSuffix = (divisor: number, suffix: string) => {
    const raw = (value / divisor).toFixed(digits);
    const trimmed = trimTrailingZeros ? raw.replace(/\.0+$|(?<=\.\d*[1-9])0+$/g, '').replace(/\.$/, '') : raw;
    return `${trimmed}${suffix}`;
  };

  if (absolute >= 1_000_000_000) {
    return formatWithSuffix(1_000_000_000, 'B');
  }

  if (absolute >= 1_000_000) {
    return formatWithSuffix(1_000_000, 'M');
  }

  if (absolute >= 1_000) {
    return formatWithSuffix(1_000, 'K');
  }

  const num = parseFloat(value.toFixed(digits));
  return num.toLocaleString(undefined, {
    maximumFractionDigits: digits,
    minimumFractionDigits: trimTrailingZeros ? 0 : digits,
  });
}

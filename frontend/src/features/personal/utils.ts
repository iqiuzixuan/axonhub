export type UsagePeriod = '7' | '30' | 'month' | 'custom';

export function getPersonalDateRange(today: string, period: UsagePeriod, custom: { start: string; end: string }) {
  if (period === 'custom') return custom;
  if (period === 'month') return { start: today.slice(0, 8) + '01', end: today };
  const start = new Date(today + 'T00:00:00Z');
  start.setUTCDate(start.getUTCDate() - (period === '7' ? 6 : 29));
  return { start: start.toISOString().slice(0, 10), end: today };
}

export function isPersonalDateRangeValid(start: string, end: string) {
  const dates = [start, end].map((date) => new Date(date + 'T00:00:00Z'));
  if (dates.some((date, i) => Number.isNaN(date.valueOf()) || date.toISOString().slice(0, 10) !== [start, end][i])) return false;
  const days = (dates[1].valueOf() - dates[0].valueOf()) / 86_400_000;
  return days >= 0 && days < 93;
}

// Escape formula-looking labels as well as RFC 4180 quotes/newlines. Names are
// user-controlled even though the data has already passed backend ownership checks.
export function personalUsageCSV(rows: (string | number)[][]) {
  return (
    '\uFEFF' +
    rows
      .map((row) =>
        row
          .map((value) => {
            const safe = typeof value === 'string' && /^[\s]*[=+@-]/.test(value) ? "'" + value : String(value);
            return '"' + safe.replaceAll('"', '""') + '"';
          })
          .join(',')
      )
      .join('\r\n')
  );
}

export function personalModelExample(modelId: string, type: string | null, baseURL: string, language: string) {
  const embedding = type === 'embedding';
  if (language === 'python') {
    return [
      'from openai import OpenAI',
      'import os',
      '',
      'client = OpenAI(',
      '    base_url=' + JSON.stringify(baseURL) + ',',
      '    api_key=os.environ["AXONHUB_API_KEY"],',
      ')',
      '',
      'response = client.' + (embedding ? 'embeddings' : 'chat.completions') + '.create(',
      '    model=' + JSON.stringify(modelId) + ',',
      embedding ? '    input="Hello",' : '    messages=[{"role": "user", "content": "Hello"}],',
      ')',
      'print(response)',
    ].join('\n');
  }
  const payload = embedding ? { model: modelId, input: 'Hello' } : { model: modelId, messages: [{ role: 'user', content: 'Hello' }] };
  const quote = (text: string) => "'" + text.replaceAll("'", "'\\''") + "'";
  return [
    'curl ' + quote(baseURL + (embedding ? '/embeddings' : '/chat/completions')),
    '  -H "Authorization: Bearer $AXONHUB_API_KEY"',
    '  -H "Content-Type: application/json"',
    '  -d ' + quote(JSON.stringify(payload, null, 2)),
  ].join(' \\\n');
}

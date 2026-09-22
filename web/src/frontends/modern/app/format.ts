import { numberFormatter } from '@modern/components/ui/intl-formatters'

// Client-area balances and costs are nano-USD (1e-9 USD) integers; display
// them with micro-USD precision so small pay-as-you-go amounts stay readable.
export function formatNanoUSD(value: number, locale: string): string {
  const micro = (BigInt(Math.trunc(value)) + 500n) / 1000n
  return numberFormatter(locale, {
    style: 'currency',
    currency: 'USD',
    currencyDisplay: 'symbol',
    minimumFractionDigits: 6,
    maximumFractionDigits: 6,
  }).format(Number(micro) / 1_000_000)
}

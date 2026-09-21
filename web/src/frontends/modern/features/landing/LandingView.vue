<script setup lang="ts">
import { ArrowRight, ChartNoAxesCombined, Coins, ShieldCheck, Zap } from '@lucide/vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { getLandingPublic } from '@modern/api/landing-public'
import { AppButton, AppIcon } from '@modern/components/ui'
import { numberFormatter } from '@modern/components/ui/intl-formatters'
import { useApiClient } from '@shared/http/client-context'

const client = useApiClient()

const models = ref<string[]>([])
const stats = ref<{ totalRequests: number; totalTokens: number } | null>(null)
const loading = ref(true)
const failed = ref(false)

let controller: AbortController | null = null

onMounted(async () => {
  controller = new AbortController()
  try {
    const data = await getLandingPublic(client, controller.signal)
    models.value = data.models
    stats.value = data.stats
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  controller?.abort()
})

const idNumber = numberFormatter('id-ID')
const totalRequests = computed(() =>
  loading.value ? '—' : stats.value ? idNumber.format(stats.value.totalRequests) : '—',
)
const totalTokens = computed(() =>
  loading.value ? '—' : stats.value ? idNumber.format(stats.value.totalTokens) : '—',
)
const modelCount = computed(() => (loading.value ? '—' : idNumber.format(models.value.length)))

const features = [
  {
    icon: Coins,
    title: 'Bayar per Token',
    description:
      'Tidak ada langganan. Saldo hanya terpotong sesuai jumlah token yang benar-benar Anda pakai.',
  },
  {
    icon: Zap,
    title: 'Model Frontier Terbaru',
    description:
      'Akses model-model frontier terbaru dari OpenAI, Anthropic, Google, dan lainnya lewat satu API.',
  },
  {
    icon: ShieldCheck,
    title: 'Aman & Terpisah per Kunci',
    description: 'Setiap kunci API terisolasi dengan kuota dan pemakaian yang terpisah satu sama lain.',
  },
  {
    icon: ChartNoAxesCombined,
    title: 'Pantau Pemakaian Real-time',
    description: 'Lihat statistik request dan token secara real-time langsung dari konsol.',
  },
]

const steps = [
  {
    title: 'Daftar & buat kunci API',
    description: 'Buat akun, lalu terbitkan kunci API pertama Anda dalam hitungan detik.',
  },
  {
    title: 'Isi saldo (Rupiah)',
    description: 'Top up saldo dalam Rupiah — tanpa konversi mata uang asing yang merepotkan.',
  },
  {
    title: 'Panggil satu endpoint untuk semua model',
    description: 'Satu endpoint kompatibel untuk semua model frontier. Ganti model cukup lewat parameter.',
  },
]

const faqs = [
  {
    question: 'Bagaimana cara membayar?',
    answer:
      'Isi saldo dalam Rupiah melalui konsol. Saldo otomatis terpotong per token sesuai harga model yang Anda pakai.',
  },
  {
    question: 'Model apa saja yang didukung?',
    answer:
      'Tokenizer mendukung model frontier seperti GPT, Claude, dan Gemini. Daftar lengkap tersedia di bagian Model di atas.',
  },
  {
    question: 'Apakah ada biaya langganan?',
    answer: 'Tidak. Tokenizer murni pay-as-you-go — Anda hanya membayar token yang benar-benar dipakai.',
  },
  {
    question: 'Apakah ada batas pemakaian?',
    answer:
      'Tidak ada batas bulanan. Selama saldo mencukupi, Anda bebas memanggil model mana pun kapan pun.',
  },
]
</script>

<template>
  <div class="modern-landing">
    <section class="modern-landing-hero">
      <p class="modern-landing-badge">PAYG · AI Frontier · Indonesia</p>
      <h1 class="modern-landing-tagline">Akses Model AI Frontier, Bayar Sesuai Pemakaian</h1>
      <p class="modern-landing-description">
        Satu API untuk GPT, Claude, Gemini, dan model frontier lainnya. Tanpa langganan — cukup isi
        saldo dalam Rupiah dan bayar sesuai token yang dipakai.
      </p>
      <div class="modern-landing-actions">
        <AppButton variant="primary" as-child>
          <RouterLink :to="{ name: 'modern-login' }">
            Masuk Konsol
            <AppIcon :icon="ArrowRight" size="md" />
          </RouterLink>
        </AppButton>
        <AppButton variant="outline" as-child>
          <a href="#models">Lihat Model</a>
        </AppButton>
      </div>
    </section>

    <section v-if="!failed" class="modern-landing-stats" aria-label="Statistik Tokenizer">
      <div class="modern-landing-stat">
        <p class="modern-landing-stat-label">Total Request</p>
        <p class="modern-landing-stat-value">{{ totalRequests }}</p>
      </div>
      <div class="modern-landing-stat">
        <p class="modern-landing-stat-label">Total Token</p>
        <p class="modern-landing-stat-value">{{ totalTokens }}</p>
      </div>
      <div class="modern-landing-stat">
        <p class="modern-landing-stat-label">Model Tersedia</p>
        <p class="modern-landing-stat-value">{{ modelCount }}</p>
      </div>
    </section>

    <section id="models" class="modern-landing-section">
      <h2 class="modern-landing-section-title">Model Frontier yang Tersedia</h2>
      <p v-if="failed || models.length === 0" class="modern-landing-models-fallback">
        Model akan segera diumumkan.
      </p>
      <div v-else class="modern-landing-models">
        <span v-for="model in models" :key="model" class="modern-landing-model-chip">
          {{ model }}
        </span>
      </div>
    </section>

    <section class="modern-landing-section">
      <h2 class="modern-landing-section-title">Kenapa Tokenizer</h2>
      <div class="modern-landing-features">
        <div v-for="feature in features" :key="feature.title" class="modern-landing-feature">
          <div class="modern-landing-feature-icon">
            <AppIcon :icon="feature.icon" size="lg" />
          </div>
          <h3 class="modern-landing-feature-title">{{ feature.title }}</h3>
          <p class="modern-landing-feature-description">{{ feature.description }}</p>
        </div>
      </div>
    </section>

    <section class="modern-landing-section">
      <h2 class="modern-landing-section-title">Cara Kerja</h2>
      <ol class="modern-landing-steps">
        <li v-for="(step, index) in steps" :key="step.title" class="modern-landing-step">
          <span class="modern-landing-step-number">{{ index + 1 }}</span>
          <div>
            <h3 class="modern-landing-step-title">{{ step.title }}</h3>
            <p class="modern-landing-step-description">{{ step.description }}</p>
          </div>
        </li>
      </ol>
    </section>

    <section id="harga" class="modern-landing-section">
      <div class="modern-landing-pricing">
        <h2 class="modern-landing-section-title">Harga</h2>
        <p class="modern-landing-pricing-text">
          Tanpa langganan. Saldo terpotong per token sesuai harga model yang dipakai.
        </p>
        <AppButton variant="primary" as-child>
          <RouterLink :to="{ name: 'modern-login' }">
            Lihat harga di konsol
            <AppIcon :icon="ArrowRight" size="md" />
          </RouterLink>
        </AppButton>
      </div>
    </section>

    <section id="faq" class="modern-landing-section">
      <h2 class="modern-landing-section-title">FAQ</h2>
      <div class="modern-landing-faqs">
        <details v-for="faq in faqs" :key="faq.question" class="modern-landing-faq">
          <summary class="modern-landing-faq-question">{{ faq.question }}</summary>
          <p class="modern-landing-faq-answer">{{ faq.answer }}</p>
        </details>
      </div>
    </section>

    <footer class="modern-landing-footer">
      <span>© 2026 Tokenizer</span>
      <span aria-hidden="true">·</span>
      <RouterLink :to="{ name: 'modern-login' }">Masuk</RouterLink>
    </footer>
  </div>
</template>

<style scoped>
.modern-landing {
  display: grid;
  width: min(100%, 1040px);
  gap: var(--modern-space-12);
  padding: var(--modern-space-8) var(--modern-content-inset);
}
.modern-landing-hero {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  text-align: center;
}
.modern-landing-badge {
  color: var(--modern-accent);
  font-size: var(--modern-font-size-small);
  font-weight: var(--modern-weight-medium);
}
.modern-landing-tagline {
  max-width: 22em;
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  line-height: var(--modern-leading-title);
}
.modern-landing-description {
  max-width: 42em;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-body);
}
.modern-landing-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: var(--modern-space-3);
  margin-top: var(--modern-space-2);
}
.modern-landing-actions a {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: inherit;
  text-decoration: none;
}
.modern-landing-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  gap: var(--modern-space-4);
}
.modern-landing-stat {
  display: grid;
  gap: var(--modern-space-1);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-landing-stat-label {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.modern-landing-stat-value {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  font-variant-numeric: tabular-nums;
}
.modern-landing-section {
  display: grid;
  gap: var(--modern-space-5);
  scroll-margin-top: calc(var(--modern-topbar-height) + var(--modern-space-4));
}
.modern-landing-section-title {
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
}
.modern-landing-models {
  display: flex;
  flex-wrap: wrap;
  gap: var(--modern-space-2);
}
.modern-landing-model-chip {
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-control);
  background: var(--modern-surface);
  padding: var(--modern-space-1-5) var(--modern-space-3);
  font-size: var(--modern-font-size-small);
}
.modern-landing-models-fallback {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-body);
}
.modern-landing-features {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  gap: var(--modern-space-4);
}
.modern-landing-feature {
  display: grid;
  gap: var(--modern-space-3);
  align-content: start;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-5);
}
.modern-landing-feature-icon {
  display: inline-flex;
  width: fit-content;
  border-radius: var(--modern-radius-control);
  background: var(--modern-subtle);
  padding: var(--modern-space-2);
  color: var(--modern-accent);
}
.modern-landing-feature-title {
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-medium);
}
.modern-landing-feature-description {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-landing-steps {
  display: grid;
  gap: var(--modern-space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}
.modern-landing-step {
  display: flex;
  align-items: flex-start;
  gap: var(--modern-space-3);
}
.modern-landing-step-number {
  display: inline-flex;
  width: 2rem;
  height: 2rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--modern-subtle);
  color: var(--modern-accent);
  font-size: var(--modern-font-size-small);
  font-weight: var(--modern-weight-semibold);
}
.modern-landing-step-title {
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-medium);
}
.modern-landing-step-description {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-landing-pricing {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-4);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-8) var(--modern-space-5);
  text-align: center;
}
.modern-landing-pricing-text {
  max-width: 36em;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-body);
}
.modern-landing-pricing a {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1-5);
  color: inherit;
  text-decoration: none;
}
.modern-landing-faqs {
  display: grid;
  gap: var(--modern-space-2);
}
.modern-landing-faq {
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-4) var(--modern-space-5);
}
.modern-landing-faq-question {
  cursor: pointer;
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-medium);
}
.modern-landing-faq-answer {
  margin-top: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-landing-footer {
  display: flex;
  justify-content: center;
  gap: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}
.modern-landing-footer a {
  color: var(--modern-accent);
  text-decoration: none;
}
.modern-landing-footer a:hover {
  text-decoration: underline;
}
</style>

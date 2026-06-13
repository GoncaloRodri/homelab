# k3s Home Lab

- author: @GoncaloRodri

---

## Finance App

A self-hosted personal finance dashboard running on k3s. Tracks transactions, budgets, and investments — with the goal of becoming a full financial co-pilot.

### Current features

- Import bank transactions via CSV (CGD, Trade Republic, generic)
- Categorise transactions with auto-categorisation and colour-coded badges
- Monthly budget tracking with progress bars (over-budget alerts)
- Dashboard with income vs expense split and 90-day balance trend
- Monthly reports (stacked bar chart, 12-month breakdown)
- Spending projections based on 6-month average
- Investment portfolio with live prices via Yahoo Finance and a 3-D allocation chart
- Dark / light mode with system preference detection
- Read-only sharing between users

---

## Architecture

### Deployment

GitHub Actions CI/CD. Each app has its own workflow triggered by path filters (`apps/<name>/**`) so a change to the finance service does not rebuild or redeploy unrelated apps.

### Apps and services

Each app lives under `apps/<name>/` and follows a shared layout:

```
apps/<name>/
  services/
    api/          # Go service
  k8s/            # Kubernetes manifests (deployment, service, ingress)
  .github/        # App-specific CI workflow (if separate from root)
```

### Database

All apps share a single MongoDB instance but each app owns a **dedicated database**: `homelab_finance`, `homelab_smarthome`, etc. The `users` service writes to `homelab` and is the canonical auth source — other apps query the `users` collection directly rather than making HTTP calls between services.

### Auth

A shared `users` service handles registration and login. Apps that need to identify the current user resolve the session against the shared MongoDB `users` collection.

### Secrets

Kubernetes Secrets managed manually with `kubectl`. Secrets are never committed to git — `.gitignore` covers `*.env` and any manifest containing literal credentials.

### Adding a new app

Copy an existing app directory as a starting point. Conventions to follow:

- Use the app's own MongoDB database (not the shared `homelab` database)
- Add a path-filtered GitHub Actions workflow under `.github/workflows/<name>.yml`
- Place k8s manifests under `apps/<name>/k8s/` with at minimum: `deployment.yaml`, `service.yaml`, `ingress.yaml`
- Read the MongoDB URI and any credentials from environment variables injected by Kubernetes Secrets

---

## Roadmap

The main goal is to evolve from a **ledger** (records what happened) into a **financial co-pilot** (tells you what to do next, based on where you want to end up).

### Phase 1 — Dashboard redesign + disposable income number *(next)*

Replace the current information-dense dashboard with a focused layout that immediately answers the questions that matter:

**Hero block — "Available to spend until payday"**
- A single, always-visible number calculated as: `Income − fixed expenses − goal contributions − category budgets`
- Month-progress bar showing how much of the disposable budget has been used

**3 diagnostic cards**
- *Bank balance should be* — exact breakdown: rent + bills + subscriptions + investments + safety buffer = minimum recommended balance right now
- *Savings rate* — % of income saved, with month-over-month delta
- *Portfolio today* — total market value and today's change

**Supporting panels (below the fold)**
- *Stocks at a glance* — per-ETF value and P&L, without navigating to the full portfolio page
- *Budget health* — thin progress bars per category, highlighted when near or over limit
- *Recent activity* — last 4–5 transactions with colored category dots

**Underlying data work**
- Auto-detect fixed/recurring expenses from transaction history (rent, subscriptions, utilities)
- Separate fixed costs from variable spend so budgets only cover what you can actually control
- Savings rate trend (% of income saved each month)

---

### Phase 2 — Goals: explore mode

Before committing to a financial goal, plan it first.

- Goal types: **one-off purchase** (Nintendo Switch, laptop, holiday), **deposit / down-payment** (house, apartment), **emergency fund** (auto-sized to N months of your average spend), **recurring investment** (links to the portfolio)
- Input: target amount + deadline
- Output: monthly contribution needed, impact on disposable income, months to target at current savings rate
- No commitment required — explore as many scenarios as you want

---

### Phase 3 — Goals: commit + plan

- Committed goals generate a monthly "reservation" deducted from your disposable income
- Conflict detection: "you can't fund both the Switch by June and the house deposit by 2028 at this savings rate"
- Goal progress tracking over time

---

### Phase 4 — Net worth

- Cash accounts + portfolio market value − credit card balances = net worth
- Historical net worth chart (all data already exists, just needs aggregation)

---

### Phase 5 — "What if" simulator + savings rate

- Drag a slider for income change, a one-off expense, or a new goal — see how it ripples through disposable income and goal timelines
- Savings rate history and trend once several months of goal data exist

---

### Phase 6 — Alerts and nudges

- "You've spent 80% of your Food budget and it's only the 15th"
- "You're on track to miss your Switch goal by 2 months"
- Delivered as dashboard banners initially, push notifications or email later

---

### Future / backlog

- **Household mode** — merge incomes and goals for couples planning together (extends the existing sharing feature)
- **Automatic transaction import** — scheduled CSV fetch or open banking integration
- **Tax summary** — annual income, investment gains, and deductible expenses export

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

## Roadmap

The main goal is to evolve from a **ledger** (records what happened) into a **financial co-pilot** (tells you what to do next, based on where you want to end up).

### Phase 1 — Disposable income number *(next)*

A single, always-visible number: **"You have €X to spend freely this month."**

Calculated as: `Income − fixed expenses − goal contributions − category budgets`

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

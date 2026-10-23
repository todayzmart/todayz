# Demo pack, 23 October 2026

Start with [PROGRESS.md](PROGRESS.md). It covers what's built, quality, decisions needed, and the weekend plan.

## Live, on the build machine (`make dev`)

| What | Address | Notes |
|---|---|---|
| Marketing site | https://todayz.localhost | English is final. Add `/hi/`, `/kn/`, `/te/` or `/ta/` for the machine-drafted languages. |
| Shop owner's dashboard | https://business.todayz.localhost | Sample data, labelled as such. Buttons show a notice and change nothing. |
| Customer app | https://app.todayz.localhost | Shell only. The screens arrive this weekend. |
| Monitoring | https://grafana.todayz.localhost | Local Grafana |

All four answered 200 at 17:20 on 23 October.

## Screenshots

**Dashboard**
- [desktop](../B-03-demo/overview-desktop.png)
- [phone](../B-03-demo/overview-phone.png)

**Site, home page**
- [1440 px](../W1-09/screens/home-1440.png)
- [390 px](../W1-09/screens/home-390.png)
- [Hindi](../W1-09/screens/home-hi-1440.png)

**Site, for retailers**
- [1440 px](../W1-09/screens/for-retailers-1440.png)
- [390 px](../W1-09/screens/for-retailers-390.png)

**Other screens**
- every site page: `docs/qa/W1-09/screens/`
- all 32 design-system components: `docs/qa/W1-02/screens/`

## In progress (Friday evening)

Four packages are running for the weekend core loop:
- sign-in;
- background jobs;
- monitoring;
- the cloud setup files (written, not applied).

The cloud files are written and nearly through their checks. They join the main build after the freeze.

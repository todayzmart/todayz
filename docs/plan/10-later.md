# 10 · Later (agreed, out of v1)

| Item | Notes | Prerequisite |
|---|---|---|
| **Catalogue lists from Blinkit, PharmEasy, Nykaa and electronics sites** (user TODO) | Use them to seed the master catalogue by category for onboarding. Risks: these sites' terms of service usually forbid scraping, and product images and descriptions are copyrighted. Safer routes, in order: (1) brand/distributor product feeds and GS1 India barcode data; (2) open data (Open Food Facts India); (3) our own photos via the shelf-photo flow (AI-R3) and supplier bill scans (AI-R2), which build the catalogue from real shops; (4) scraping only product names/pack sizes for matching (no images or descriptions) after a legal check. Decision needed from the user before any scraping. | Legal view |
| **Salespeople adding products per retailer** (user TODO) | Covered in v1 by field agents (A-04) adding products one by one; later: a bulk "visit mode" with barcode scanning on the agent's phone and offline support | v1 field agent flows |
| **Billing for retailers** | Free for now. Later: subscription tiers (POS free, premium insights/coach/banners), or commission on online orders. Needs GST invoicing and a payment gateway (Razorpay/Cashfree) for retailer subscriptions. | Traction data |
| **Storefronts** | Per-shop page at `todayz.in/s/<slug>` (SEO, shareable) first, custom subdomain later; uses the same shop data | v1 shop page |
| Payment gateway for customers | UPI intent/collect through a gateway, so credits and payment settle together; refunds | Volume + regulatory review |
| Khata (credit to customers) | Shop-side ledger of customer dues on the POS | POS adoption |
| Bluetooth ESC/POS receipt printers | Web Bluetooth where supported, or a tiny Android wrapper | POS adoption |
| Native app wrappers | Capacitor/TWA wrappers for Play Store presence and better push on iOS | PWA traction |
| Demand data for brands | Aggregated, anonymised demand reports (k-anonymity ≥ 10) sold to brands/distributors | Enough data; DPDP review |
| Dedicated search engine | Meilisearch/OpenSearch if Postgres search p95 > 200 ms at scale | Load data |
| ClickHouse for events | If events > ~5M/day | Volume |
| Multi-AZ RDS, read replica for analytics | Reliability and isolation | Revenue |
| AI later items | Ask about my shop (AI-R7), distributor restock lists (AI-R8), help assistant (AI-C4), AI substitutes ranking, Ask Pulse (AI-A4), KYC document check (AI-A5) | v1 AI adoption data |
| Dark theme | Removed at the user's request; only if asked | User request |

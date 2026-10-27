# TodayZ docs

A map of every doc: what each holds and when to read it. New to the repo? Read the root [README.md](../README.md) first, then the **Guide** in order.

## Guide: how TodayZ is built (as built, kept current)

| Doc | Read it to learn |
|---|---|
| [architecture.md](guide/architecture.md) | The system: apps, API, worker, database, jobs, realtime, offline POS, AI and observability, with diagrams |
| [flows.md](guide/flows.md) | Each main journey step by step, with sequence and state diagrams: sign-in, search, book, pickup, delivery, POS, credits, payouts, asks, approvals |
| [code-structure.md](guide/code-structure.md) | Where code lives: every folder, every backend package, how an app is laid out, the shared packages, mk files and scripts |
| [local-dev.md](guide/local-dev.md) | Setting up a Mac, running the stack, seed data, common problems |
| [testing.md](guide/testing.md) | Every test layer: where it lives, how to run it, when it runs, how to add one |
| [deployment.md](guide/deployment.md) | Environments, CI workflows, Cloudflare Pages, AWS Beanstalk, Terraform, releases, rollback, go-live status |
| [working-on-todayz.md](guide/working-on-todayz.md) | How to make a change, the Definition of done, and the rule that keeps these docs true |

## Features: what the product does

[features/README.md](features/README.md) has a feature × app matrix and one file per area. Each file covers what users can do, the rules, how it works, the code, the tests, its status and the decisions behind it.

## Plan and decisions: intent and reasons

| Doc | What it holds |
|---|---|
| [plan/README.md](plan/README.md) | The build plan: waves, work packages, roles, Definition of done |
| [plan/01-product-rules.md](plan/01-product-rules.md) | **Every product rule and number.** The single source of truth |
| [plan/12-decision-log.md](plan/12-decision-log.md) | Why things are the way they are (D-01 onwards). It wins over the numbered plan files |
| [plan/02-architecture.md](plan/02-architecture.md) … [plan/11-ai-features.md](plan/11-ai-features.md) | The original plan per area: architecture, data model, API, observability, screens, work packages, testing, infra, later, AI |
| [adr/README.md](adr/README.md) | Architecture decision records: Go, pgx/sqlc, River, SSE, Postgres search, Protomaps, own analytics, local-first |

The plan says what was intended; the guide and feature docs say what was built. When they differ, the code is right: fix the guide or feature doc, and add a decision-log entry if a rule changed.

## Running it in production

| Doc | When you need it |
|---|---|
| [ops/README.md](ops/README.md) | Severity, on-call, incidents, release checklist, secret rotation, backup and restore |
| [runbooks/README.md](runbooks/README.md) | One runbook per alert and job: what it means, first five minutes, diagnosis, fix |
| [security/threat-model.md](security/threat-model.md), [security/asvs-l2.md](security/asvs-l2.md) | Threat model and the OWASP ASVS L2 checklist |
| [qa/deploy-prep/RUNBOOK.md](qa/deploy-prep/RUNBOOK.md) | The AWS go-live runbook |

## Evidence

[qa/](qa/) holds the evidence for each work package (`qa/<WP>/REPORT.md` plus screenshots), and the release-candidate packs (`qa/rc-<date>/`). These are records of what was checked and when. They are not kept current; the guide and feature docs are.

## Design

The design system's source of truth is the [design artifact](https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF). A copy lives in [design/](../design/README.md). It uses a white page, a mint wash, pastel tints, Lucide icons and the Anek and Figtree fonts, in a light theme only.

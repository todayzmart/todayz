# AWS: everything the founder does, in order

There are three parts. Parts A and B take about 15 minutes, and after them the lead can build prod. Part C (SMS) takes days because of DLT, so start it today. Prod goes up without it; only real phone logins wait for it.

Put each value in `.env.cloud.local` as `NAME=value`, and never paste a value in chat. When a part is done, tell the lead "AWS part A done" (or B, or C).

## A. Lift the block on the TodayZ AWS account (required)

> **26 Oct: TodayZ now uses a new account, `061292478202`** (Terraform refuses any other, D-255). Part A was written for the old account `212412893706`. Do it again only if the new account is also in an AWS Organization whose service control policy blocks RDS, EC2, S3, ECR, Secrets Manager or SMS; a standalone account needs nothing here. The first deploy is `docs/qa/deploy-prep/RUNBOOK.md`.

The TodayZ account `212412893706` is a member of an AWS Organization whose management account is `882034444063`. That organization's service control policy `p-4dtreasp` denies RDS, EC2, S3, ECR, Secrets Manager and SMS in every region, so Terraform cannot create anything.

Pick **one** of these two options.

### Option 1: fix the policy (recommended, about 5 minutes)

1. Sign in to the AWS console as the **management account** `882034444063`. It is the account whose root email is a gmail address that is not your budget-alert address. Use the root user or an admin user there.
2. Open https://console.aws.amazon.com/organizations → **Policies** → **Service control policies** → open `p-4dtreasp`.
3. Look at its **Targets** tab. Then do one of these:
   - **Detach it from the TodayZ account (simplest).** Go to **AWS accounts**, open the TodayZ account (`212412893706`), open the **Policies** tab, select `p-4dtreasp` → **Detach**. If the policy is attached to the OU or the Root rather than the account, either move the TodayZ account to an OU without it (**Actions → Move**), or detach it from that OU.
   - **Or edit it to allow our region.** If the policy is a region lock (a `Deny` with a condition on `aws:RequestedRegion`), add `ap-south-1` to its allowed list. For Hyderabad, add `ap-south-2` as well.
4. Check that the TodayZ account still has **`FullAWSAccess`** attached, on the account or through its OU. Without any Allow policy, everything stays denied.

### Option 2: take the TodayZ account out of the organization

Use this if you don't control `882034444063`, or don't want TodayZ billed through it.

1. Sign in as the **root user of the TodayZ account** (`212412893706`).
2. Add a payment method (Billing → Payment preferences) and keep the Basic support plan.
3. Go to https://console.aws.amazon.com/organizations → **Leave organization**. The management account may have to allow this first.

## B. Region

- **Mumbai `ap-south-1` (recommended).** It is already switched on and has every service we use, including SMS. In `.env.cloud.local`, set `AWS_REGION=ap-south-1`. Right now it says `ap-south-2`.
- **Hyderabad `ap-south-2`.** You must switch it on: TodayZ account → account name (top right) → **Account** → **AWS Regions** → Asia Pacific (Hyderabad) → **Enable**. That takes a few minutes to an hour. The policy in part A must allow it too. Keep `AWS_REGION=ap-south-2`.

Whichever region you pick, do part C's AWS console steps in that same region.

Nothing else is needed in AWS for prod. Terraform creates the network, database, Beanstalk, ECR, Secrets Manager, backups, the GitHub deploy role and the budget alarm, which is created first. The lead shows you the plan and the monthly cost, and applies only after your yes.

## C. SMS through AWS (D-212)

India requires every business SMS to be registered on DLT first. AWS only sends what DLT has approved.

### C1. DLT registration (on a telecom portal, 2–7 days)

1. **Register as a Principal Entity** on one portal. You only need one, since they share data:
   - Jio TrueConnect: https://trueconnect.jio.com
   - Airtel: https://dltconnect.airtel.in
   - Vi: https://www.vilpower.in

   You need the company PAN, the GST or incorporation certificate, a letter of authorisation and the fee (about ₹5,900 including GST). When approved you get an **Entity ID**, a 19-digit number.
2. **Register a Header** (the sender ID): `TODAYZ`, category **Transactional / Service Implicit**. It must be exactly 6 letters.
3. **Register these content templates.** Copy the text exactly; `{#var#}` is the placeholder the app fills in.

   | Name in our app | Type | Language | Text |
   |---|---|---|---|
   | OTP | Service Implicit, OTP | English | `{#var#} is your TodayZ login code. Do not share it with anyone.` |
   | `tz_order_unseen` | Service Implicit | English | `A new order at {#var#} has not been opened for {#var#} minutes. Open the TodayZ business app to accept it.` |
   | `tz_order_unseen_hi` | Service Implicit | Hindi (Unicode) | `{#var#} पर एक नया ऑर्डर {#var#} मिनट से खोला नहीं गया है। स्वीकार करने के लिए TodayZ बिज़नेस ऐप खोलें।` |
   | `tz_owner_code` | Service Implicit, OTP | English | `{#var#} is your TodayZ code to confirm your shop with our field agent. Share it only with that agent.` |

   Each approved template gets a **Template ID** (a long number).
4. **Telemarketer chain.** If the portal asks for a telemarketer or "chain binding", add the telemarketer IDs AWS uses for India. They are in the AWS India registration guide, or you can ask for them in the support case in C3.

### C2. Fill in `.env.cloud.local`

```
SMS_SENDER_ID=TODAYZ
SMS_DLT_ENTITY_ID=<Entity ID>
SMS_OTP_TEMPLATE_ID=<OTP template ID>
SMS_NOTICE_TEMPLATE_IDS=tz_order_unseen=<ID>,tz_order_unseen_hi=<ID>,tz_owner_code=<ID>
```

These are not secret, but keep them in that file anyway. There is no SMS key: the API signs with its AWS role.

### C3. AWS End User Messaging console (TodayZ account, our region)

Open https://console.aws.amazon.com/sms-voice/ (it is called **AWS End User Messaging SMS**) and check that the region selector at the top shows the region from part B.

1. **Test numbers (sandbox).** A new account can only text verified numbers. Go to **Destination phone numbers** → **Add destination phone number** → your own mobile → enter the code it sends. Add 2–3 numbers of people who will test.
2. **Sender ID.** Go to **Sender IDs** → **Request sender ID** → country **India**, sender ID `TODAYZ`, message type **Transactional**. If the console asks for a registration instead, fill it in with the DLT Entity ID and header.
3. **Production access and spend limit, in one support case.** Go to **Account settings** (or the sandbox banner) → **Request production access**. That opens a support case.
   - Use case: one-time login codes and order alerts to shop owners, India, transactional, no marketing.
   - Expected volume: about 1,000 messages a month to start.
   - Opt-in: the user types their number on our sign-in page.
   - DLT: give the Entity ID, header `TODAYZ` and the template IDs.
   - In the same case, ask to raise the **monthly SMS spend limit** to USD 50. The default is USD 1, which runs out after a few hundred texts.
   - If you need the telemarketer IDs for C1 step 4, ask here.

   AWS usually answers in 1–3 days.

When C2 is filled in and C3 is approved, tell the lead "SMS ready". The lead puts the IDs into prod config and sends a test OTP to a number you verified.

## Already done, nothing to do

- GitHub (`todayzmart`), Cloudflare (zone, token, R2 enabled), the budget alert email.
- Turnstile, R2 buckets, DNS, Pages: Terraform creates them.

After go-live, rotate the GitHub token and the Cloudflare token, since both were shared during setup.

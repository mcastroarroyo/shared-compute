# Community go-to-market plan: 1,000 devices online

Written 2026-09-11. Goal: 1,000 devices online at the same time, idle and serving. Baseline
today: 3 users, 10 devices ever benchmarked, 1 online.

## The arithmetic

A device is online when its owner leaves the provider running. Measured on our own fleet,
a phone is plugged in and on Wi-Fi about a third of the day and a Mac or PC about half. To
have 1,000 online at once we need roughly 2,500 to 3,000 devices installed and kept, which
at a 40% install-to-keep rate means about 7,000 sign-ups. That is the funnel this plan
feeds. Every number below is a target, not a promise.

| Milestone | Devices online | By | What gets us there |
|---|---|---|---|
| M1 | 25 | week 2 | personal network, first posts, open testing live |
| M2 | 100 | week 5 | second wave of posts, referral loop, Product Hunt |
| M3 | 300 | week 10 | university clubs and hackathons, LAN-cluster owners, first buyer workloads |
| M4 | 1,000 | month 5 to 6 | paid workloads daily, weekly release cadence, contributor community |

If M1 slips by more than a week, the message is wrong, not the channel. Re-read the feedback
before posting more.

## Who we are recruiting, in order of fit

1. **People who already run local models** (r/LocalLLaMA, llama.cpp and Ollama communities, PAIR
   users). They have the hardware, they understand privacy, and one command is nothing to them.
   Highest yield per post.
2. **Homelab and self-host owners** (r/selfhosted, r/homelab, Discords). Machines idle by
   design; they like a binary that dials out and accepts nothing inbound.
3. **Android enthusiasts** (r/androidapps, r/Android, XDA). Volume, and the pairing flow is
   built for them. Needs the Play open-testing track live first.
4. **Students and clubs** (CS societies, hackathons). Many phones and laptops per contact, and a
   mission they respond to.
5. **Security and ops people who would also buy** (via the MCP server and the log-triage
   example). Fewer devices, but they bring the workloads that keep everyone else plugged in.

## What the agent executes without waiting

These need no account of the founder's and start now, in this order.

1. **Live fleet counter on the site and testers page.** Devices online, devices ever, jobs
   today, from the public health endpoint. Social proof, and it makes "join the community"
   concrete. Also a shareable status page at ayni-ai.com/status.
2. **Referral link in the web app.** Each signed-in user gets `app.ayni-ai.com/join/<code>`.
   Referrals are counted on the user's page and on a public "founding devices" leaderboard
   (display names only, opt-in). Every recruitment message carries the founder's code.
3. **Founding device recognition.** The first 1,000 devices get a permanent "founding" badge
   on the earnings page and a named entry in the repository's FOUNDERS.md if the owner opts in.
   Cheap, honest, and the kind of thing people screenshot.
4. **Weekly cadence.** Every Monday: a release note in GitHub Discussions Announcements, the
   fleet numbers, what shipped from tester feedback, and who was credited. The agent drafts and
   posts these on the project's own GitHub, which is Ayni's channel.
5. **Reply to every tester within the tick.** Feedback threads answered in-app, labelled, and
   turned into issues. Fixes ship; the tester is told. This is already running.
6. **Test jobs for every device within minutes.** The pulse (already running) sends a job to
   each new device so joining feels real. Daily budget rises with the fleet.
7. **Content that answers the questions people actually ask.** Three short pages on the site:
   "What runs on my device and what does it cost me" (battery, data, thermals, measured),
   "How a job is sealed" (the crypto envelope, plain language), and "What I earn and when"
   (rate card, payout threshold, timing). Each one is a link to drop into a comment thread.
8. **A reply kit.** Ready answers to the twelve questions every thread asks (is it crypto, does
   it read my prompts, why cents, what about my battery, is the binary signed, can I audit it),
   kept in docs/TESTER-PROGRAM.md so answers are consistent whoever posts them.
9. **Distribution beyond Play.** The provider binaries are on GitHub Releases; add a Homebrew
   tap and an APK download for people who avoid the store, both signed.
10. **Buyer side.** Publish the MCP server on the MCP registry and PyPI so security people can
    find it, and run the log-triage example as a public demo. Every paid workload is a reason
    for devices to stay online.

## What the agent executes once the founder unlocks it

Each post is written, filled into the form by the agent in the founder's browser, and
submitted only after the founder says yes to that post. The founder logs in; the agent never
types credentials.

| Channel | When | Post |
|---|---|---|
| r/LocalLLaMA | first, Tuesday to Thursday morning US time | draft A |
| Show HN | same week, 8 to 10 am Eastern, Tuesday to Thursday | draft D |
| r/selfhosted, then r/homelab a week later | week 1 and 2 | draft B |
| r/androidapps, r/Android | only after open testing is live | draft C |
| Ollama and llama.cpp Discords, Reddit LLM Discords | week 2 | draft A, shortened, no link in the first message |
| Product Hunt | at M2, with the referral loop and counter live | draft in TESTER-PROGRAM.md |
| LinkedIn and X from the founder's account | weekly, the Monday note | the release note |
| University CS clubs and hackathon organisers | week 4 onward | email, personal draft E adapted |

Every thread is watched for two hours after posting and every comment answered from the reply
kit. A thread with no replies inside two hours is a signal about the message, and the next
post waits for the fix.

## What only the founder can do

1. Log in to Reddit and Hacker News in the Chrome the agent drives.
2. Finish Play open testing (countries, promote version code 8, send for review). Until then
   phone recruitment is capped at 100 by list.
3. Personal messages to the first fifty people. Highest conversion of any channel; the agent
   drafts them, the founder sends them.
4. Approve each public post.

## How we know it is working

Tracked every tick from `GET /admin/overview` and logged to the tester program doc weekly:
sign-ups, devices paired, devices online at the daily peak, devices still online after seven
days, feedback threads and median reply time, pulse jobs served, and paid workload items.
The single number that matters is devices online at the daily peak. If a channel produces
sign-ups but not paired devices, the onboarding is the problem; if it produces paired devices
that leave within a week, the pulse and the earnings page are the problem.

## What we will not do

Pay for installs, promise income, post from accounts we do not own, post the same text in
more than one place on the same day, or hide the model size and the current fleet size.

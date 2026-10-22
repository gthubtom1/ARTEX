# Merged Skills 知识层（MIT）

> 由 `scripts/import_merged_skills.py` 从外部 Pentest-Skills-Merged 导入。
> 只收录 license=MIT 的技能全文；**不进系统提示词**，由 `kb_search` / 知识库页按需检索。
> 执行层精选见仓库 `skills/` 下同名目录（默认绑定 worker/pentest）。

- 收录：**138** 个（MIT）
- 未收录：**98** 个（license 缺失/非 MIT，见 `EXCLUDED.md`）

| 技能 | 分类 | 字节 | 描述（截断） |
|---|---|---|---|
| `osint-methodology` | redteam | 96526 | Comprehensive OSINT methodology for external red-team operations and authorized attack-surface assessments. Covers the 5-stage recon pipeline (seed discovery, asset expansion, enri… |
| `bug-bounty` | redteam | 80498 | Master bug bounty orchestrator — full pipeline: recon, pre-hunt learning, vulnerability hunting (30+ classes), A-to-B chaining, AI/LLM testing (ASI01-ASI10), language-specific grep… |
| `bb-local-toolkit` | redteam | 77832 | Complete bug bounty workflow — recon (subdomain enumeration, asset discovery, fingerprinting, HackerOne scope, source code audit), pre-hunt learning, vuln hunting (30+ classes), A-… |
| `wstg-web-pentest` | - | 45386 | Full WSTG-aligned web application pentest — 12-phase methodology from information gathering through reporting, with concrete commands, expected outputs, pitfalls, and verification … |
| `hunt-race-condition` | redteam | 45251 | Hunting skill for race condition vulnerabilities. Built from 12 public bug bounty reports including modern HTTP/2 single-packet attack cases (James Kettle DEF CON 2023 "Smashing th… |
| `hunt-auth-bypass` | redteam | 36780 | Hunting skill for auth bypass vulnerabilities. Built from 12 public bug bounty reports across SAML XSW / parser-differential (GitHub Enterprise CVE-2025-25291/25292), SAML signatur… |
| `hunt-sharepoint` | redteam | 34473 | Hunt Microsoft SharePoint Server (2013/2016/2019/Subscription Edition) on-prem farms — anonymous endpoint enumeration, version disclosure, legacy SOAP login bypass (Authentication.… |
| `bb-methodology` | redteam | 33771 | Use at the START of any bug bounty hunting session, when switching targets, or when feeling lost about what to do next. Master orchestrator that combines the 5-phase non-linear hun… |
| `vmware-vcenter-attack` | redteam | 33371 | VMware vSphere / vCenter Server external attack matrix — version fingerprinting, the high-impact CVE chain (CVE-2021-21972 vRealize unauth file upload, CVE-2021-21985 vSAN plugin R… |
| `hunt-rce` | redteam | 31978 | Hunting skill for rce vulnerabilities. Built from 67 public bug bounty reports. Use when hunting rce on any target. |
| `hunt-xss` | redteam | 31295 | Hunting skill for xss vulnerabilities. Built from 174 public bug bounty reports. Use when hunting xss on any target. |
| `security-arsenal` | redteam | 30290 | Security payloads, bypass tables, wordlists, gf pattern names, always-rejected bug list, and conditionally-valid-with-chain table. Use when you need specific payloads for XSS/SSRF/… |
| `llm-prompt-injection` | redteam | 29110 | Use when testing an authorized LLM application for prompt injection, system-prompt exposure, unsafe tool use, or RAG data-boundary failures. |
| `hunt-oauth` | redteam | 28939 | Hunting skill for oauth vulnerabilities. Built from 19 public bug bounty reports. Use when hunting oauth on any target. |
| `hunt-llm-ai` | redteam | 28724 | Hunt LLM/AI feature bugs — prompt injection, indirect injection, exfiltration viatool-use/markdown, ASCII smuggling, agentic AI security (OWASP Agentic Apps 2026, ASI01-ASI10). Pat… |
| `hunt-http-smuggling` | redteam | 27618 | Hunt HTTP request smuggling (CL.TE, TE.CL, H2.CL, H2.TE). Cause: front-end proxy and back-end server disagree on where one request ends and the next begins (Content-Length vs Trans… |
| `bugcrowd-reporting` | redteam | 27092 | Bugcrowd-specific reporting tactics complementing report-writing: VRT category search-and-fallback strategy when no exact match exists, manual severity override when VRT defaults u… |
| `redteam-mindset` | redteam | 26881 | Red-team operator discipline — the mindset corrections that separate offensive testing from defensive WAPT. Built from authorized red-team work where conservative defaults caused m… |
| `hunt-subdomain` | redteam | 26741 | Hunting skill for subdomain takeover vulnerabilities. Includes modern provider fingerprints — Microsoft Azure DevOps `cloudapp.azure.com` regional-pool re-issue (1-click OAuth ATO … |
| `okta-attack` | redteam | 26152 | Okta-as-IdP red-team attack chain — tenant discovery, user enumeration (multiple vectors), authentication flow analysis (factors enumeration, push-notification fatigue, SMS bypass)… |
| `hunt-api-misconfig` | redteam | 25529 | Hunt API security misconfiguration — mass assignment, JWT attacks, prototype pollution, HTTP verb tampering. Mass assignment: send {is_admin:true, role:admin, verified:true} on pro… |
| `web3-audit` | redteam | 25495 | Smart contract security audit — 10 DeFi bug classes (accounting desync, access control, incomplete path, off-by-one, oracle, ERC4626, reentrancy, flash loan, signature replay, prox… |
| `hunt-misc` | redteam | 25433 | Hunting skill for misc vulnerabilities. Built from 225 public bug bounty reports. Use when hunting misc on any target. |
| `web2-recon` | redteam | 25099 | Web2 recon pipeline — subdomain enumeration (subfinder, Chaos API, assetfinder), live host discovery (dnsx, httpx), URL crawling (katana, waybackurls, gau), directory fuzzing (ffuf… |
| `hunt-xxe` | redteam | 24682 | Hunting skill for xxe vulnerabilities. Built from 10 public bug bounty reports including SVG-upload XXE, Office-doc (PPTX/DOCX) XXE, SOAP XXE, SAML AssertionConsumer XXE, blind OOB… |
| `hunt-ato` | redteam | 24247 | Hunt account takeover taxonomy — 9 distinct paths to ATO, plus chains. Paths: (1) password reset flaws (host-header injection redirects token, predictable/numeric token, Referer le… |
| `hunt-idor` | redteam | 24216 | Hunting skill for idor vulnerabilities. Built from 26 public bug bounty reports. Use when hunting idor on any target. |
| `hunt-sqli` | redteam | 23970 | Hunting skill for sqli vulnerabilities. Built from 12 public bug bounty reports including modern NoSQL injection (Rocket.Chat CVE-2021-22911 MongoDB $regex, Mongoose ORM CVE-2024-5… |
| `evidence-hygiene` | redteam | 23550 | Evidence-capture and PoC-redaction discipline for bug-bounty submissions: cookie redaction protocol (which fields to mask, Preview annotation / Burp panel hiding / DevTools workflo… |
| `report-writing` | redteam | 23517 | Bug bounty report writing for H1/Bugcrowd/Intigriti/Immunefi — report templates, human tone guidelines, impact-first writing, CVSS 3.1 scoring, title formula, impact statement form… |
| `m365-entra-attack` | redteam | 23336 | Microsoft 365 / Entra ID red-team attack chain — current 2026 reality. AADSTS code reference, user enumeration vectors (with hardening status), Smart Lockout math, Conditional Acce… |
| `hunt-aspnet` | redteam | 22879 | Hunt ASP.NET-specific surface — ViewState deserialization (signed-only vs encrypted), machineKey recovery, dual-parser MAC-bypass anti-pattern, request-validator bypass, trace.axd/… |
| `hunt-tls-network` | redteam | 22538 | Hunt TLS/SSL and DNS misconfigurations — missing HSTS (downgrade attack), weak cipher suites, expired/invalid certificates, mTLS bypass, missing SPF/DKIM/DMARC (email spoofing), DN… |
| `hunt-k8s` | redteam | 22389 | Hunt Kubernetes & Docker — API anonymous access, kubelet 10250 exec (SPDY/WebSocket, NOT plain POST) and the simpler /run primitive, etcd 2379 unauth, dashboard skip-login, RBAC mi… |
| `hunt-business-logic` | redteam | 21590 | Hunting skill for business logic vulnerabilities. Built from 12 public bug bounty reports. Covers coupon-race-stacking (Instacart, Stripe, Reverb), negative-quantity-in-cart price … |
| `password-spray-methodology` | redteam | 21454 | End-to-end password spray playbook. User enumeration, lockout detection, password pattern generation, spray execution across all protocols, error code differentials, and engagement… |
| `hunt-grpc` | redteam | 21429 | Hunt gRPC vulnerabilities — server reflection enabled (enumerate all services/methods), missing authentication / metadata-stripping on internal endpoints, plaintext gRPC over HTTP/… |
| `hunt-cors` | redteam | 21376 | Hunt CORS Misconfiguration — origin-reflection with credentials, null-origin trust, subdomain-regex bypass (unanchored vs unescaped-dot vs prefix-only), pre-flight (OPTIONS) gating… |
| `hunt-graphql` | redteam | 21348 | Hunting skill for graphql vulnerabilities. Built from 12 public bug bounty reports across IDOR via node() / GID, mutation IDOR including AI/LLM features, cross-tenant IDOR, SSRF vi… |
| `enterprise-vpn-attack` | redteam | 21184 | External SSL VPN / remote-access appliance attack matrix — Cisco ASA/AnyConnect, Fortinet FortiGate/FortiOS, Citrix NetScaler/ADC, Palo Alto GlobalProtect, Pulse Secure / Ivanti Co… |
| `apk-redteam-pipeline` | redteam | 21116 | End-to-end Android APK red-team pipeline — automated APK acquisition (Play Store + apkpure + apkmirror fallback), jadx decompilation, secret/URL/JWT/Firebase grep, pinned-cert extr… |
| `hunt-websocket` | redteam | 20579 | Hunt WebSocket vulnerabilities — Cross-Site WebSocket Hijacking (CSWSH), missing/weak Origin validation on the WS handshake, no per-message authentication, message tampering, socke… |
| `hunt-session` | redteam | 20400 | Hunt Session Management vulnerabilities — session fixation (no regeneration on login), insufficient invalidation on logout / password-change / email-change, predictable or low-entr… |
| `hunt-brute-force` | redteam | 20155 | Hunt Missing/Weak Rate Limiting — login brute force, OTP/2FA brute force (10^6 keyspace), password-reset-token brute, credential stuffing, username/email enumeration via error-stri… |
| `hunt-cicd` | redteam | 19584 | Hunt CI/CD pipeline vulnerabilities — GitHub Actions workflow injection (pull_request_target Pwnrequest + ${{ }}-into-shell), self-hosted runner poisoning, OIDC trust-policy abuse,… |
| `mid-engagement-ir-detection` | redteam | 19051 | Methodology for detecting client SOC patches, attacker activity, and security-state changes that occur DURING a red-team engagement — and converting those observations into deliver… |
| `hunt-lfi` | redteam | 18978 | Hunt Local File Inclusion (LFI), Remote File Inclusion (RFI), and Path Traversal — /etc/passwd read, log poisoning → RCE, PHP filter-chain RCE (no upload needed), php:// / data:// … |
| `hunt-ntlm-info` | redteam | 18396 | Hunt NTLM/Negotiate information disclosure on internet-reachable IIS/SharePoint/Exchange. Anonymous NTLM Type-2 challenge capture leaks NetBIOS domain, internal DNS forest, compute… |
| `recon-sector-expansion` | redteam | 18290 | Multi-sector batch domain expansion — identify untested/under-tested sectors, generate candidate company domains (national chains, franchises, regionals), filter against existing t… |
| `hunt-dom` | redteam | 18245 | Hunt client-side DOM vulnerabilities — DOM Clobbering (overwrite JS globals via HTML injection), PostMessage hijacking (missing origin check), Service Worker abuse (intercept reque… |
| `deep-invade` | recon | 18018 | Deep pentest WP: SSRF, plugin CVE, JS mine, port scan chain. |
| `hunt-firebase` | redteam | 17924 | Hunt Firebase / Firestore / GCP exploitation — Firebase API key discovery in JS bundles, anonymous auth via signUp endpoint, Firestore collection enumeration with anon key, Realtim… |
| `hunt-supabase` | redteam | 17701 | Hunt Supabase exploitation — Supabase anon key discovery in JS bundles, REST API table enumeration with anon key, Row Level Security (RLS) bypass via missing organization_id check,… |
| `hunt-ldap` | redteam | 17103 | Hunt LDAP Injection and XPath Injection — authentication bypass, blind char-by-char attribute exfiltration, AD user/group enumeration, XML-store XPath bypass. Covers the LDAP speci… |
| `redteam-report-template` | redteam | 16884 | Client-facing red-team deliverable format — codifies the Subject / Observations / Description / Impact / Recommendation / PoC structure used for external red-team engagements (not … |
| `hunt-ssti` | redteam | 16473 | Hunt server-side template injection (SSTI) across Jinja2 (Flask/Django), Twig (Symfony), Freemarker (Java), ERB (Rails), Spring, Velocity, Mako, Thymeleaf, Smarty. Detection probes… |
| `meme-coin-audit` | redteam | 16429 | Meme coin and token security audit — rug pull detection (honeypot, hidden mint, fee manipulation, LP lock bypass), Solana SPL token analysis (freeze authority, mint authority, meta… |
| `xmlrpc-exploitation` | recon | 16272 | Exploit XMLRPC multicall, pingback for brute force and SSRF. |
| `hunt-host-header` | redteam | 16262 | Hunt Host Header Injection — password reset poisoning → ATO, web cache poisoning via unkeyed Host/X-Forwarded-Host, routing-based SSRF (Host picks upstream → cloud metadata/interna… |
| `hunt-source-leak` | redteam | 15122 | Hunt source code and build artifact leakage — JavaScript source maps (.js.map) reconstructing TypeScript/ES6 source, Swagger/OpenAPI JSON endpoint discovery, .env/.git exposure, we… |
| `sector-recon-methodology` | meta | 14891 | Pick sectors, compile targets, batch recon for campaigns. |
| `firebase-supabase-attack` | recon | 14566 | Exploit Firebase/Supabase for data via JS config leak probe. |
| `cors-chain-automation` | redteam | 14398 | Use when a bounded list of authorized API endpoints needs consistent CORS triage before browser validation. |
| `hunt-saml` | redteam | 14281 | Hunt SAML / SSO attacks. Patterns: XML Signature Wrapping (XSW) — modify Assertion while keeping Signature valid by relocating signed element, comment injection in NameID (admin@ta… |
| `hunt-cloud-misconfig` | redteam | 14218 | Hunt cloud / infrastructure misconfigurations. AWS: public S3 buckets (s3:GetObject anonymous), permissive bucket policies (PutObjectAcl public-write), exposed CloudFront origin, p… |
| `wordpress-plugin-hunt` | recon | 13905 | Hunt WP plugins via REST, exploit CVEs when version known. |
| `wp-plugin-cve-hunt` | redteam | 13814 | Systematic approach to finding and testing CVEs for identified WordPress plugins. Covers plugin discovery, version extraction from multiple sources (readme.txt, assets, inline JS),… |
| `hunt-mfa-bypass` | redteam | 13650 | Hunt MFA / 2FA bypass — 7 distinct patterns. (1) MFA not enforced on sensitive endpoints (password change, email change accept without MFA challenge), (2) MFA-step skip via direct … |
| `error-log-mining` | recon | 13393 | Mine error_log for creds, paths, SQL when leak hunt finds. |
| `subdomain-enumeration` | recon | 13247 | Map subdomains via crt.sh and subfinder at recon kickoff. |
| `401-403-bypass` | - | 12932 | Use when protected HTTP routes return 401 or 403. |
| `wp-mass-recon` | recon | 12772 | Batch WP recon: users, CORS, XMLRPC, leaks across domains. |
| `hunt-nodejs` | redteam | 12534 | Hunt Node.js specific vulnerabilities — Prototype Pollution → RCE chains (lodash/merge/assign), Express trust proxy misconfiguration, child_process/eval injection, template engine … |
| `hunt-nextjs` | redteam | 12479 | Hunt Next.js specific vulnerabilities — Server Actions arbitrary function execution, Middleware auth bypass via static asset paths, ISR cache poisoning, Image Optimization SSRF (/_… |
| `hunt-springboot` | redteam | 11754 | Hunt Spring Boot specific vulnerabilities — Actuator endpoints (heapdump, env, loggers, mappings, shutdown), Spring Expression Language (SpEL) injection → RCE, H2 console RCE, Jolo… |
| `web-enumeration` | recon | 11391 | Sensitive file scanning, path traversal bypass, vHost enum, .env extract, log mining, Varnish detect |
| `hunt-open-redirect` | redteam | 11203 | Hunt Open Redirect — all types including low-impact, chained to OAuth token theft → ATO, phishing chains. URL parameter manipulation, JavaScript redirect, meta refresh, header inje… |
| `staging-subdomain-hunt` | recon | 10861 | Hunt staging via crt.sh when production is WAF-hardened. |
| `js-secrets-extraction` | recon | 10515 | Analyze JS bundles and source maps for hardcoded secrets, API keys, JWTs, and internal endpoints |
| `exchange-owa-attack` | recon | 10446 | Exchange/OWA NTLM AD leak, spray attack when mail subdomain. |
| `saml-sso-attack` | auth | 10306 | Attack SAML SSO via XSW, signature strip, metadata extract. |
| `jwt-attack` | recon | 10118 | Decode, forge, brute JWTs when Bearer auth header is seen. |
| `cache-attack` | recon | 10081 | Poison CDN cache or deceive when X-Cache header is detected. |
| `hunt-file-upload` | redteam | 10045 | Hunt file upload bugs — RCE via webshell, XSS via SVG/HTML, SSRF via XXE in DOCX, path traversal via filename. Bypass tables (10 techniques): double extension (shell.php.jpg if ser… |
| `hunt-laravel` | redteam | 9942 | Hunt Laravel specific vulnerabilities — Debug mode leakage (APP_DEBUG=true exposes full stack trace + env vars), Laravel Telescope/Horizon dashboard unauthorized access, Ignition R… |
| `ops-proxyns` | redteam | 9798 | Kernel-level proxy protection via proxy-ns — forces ALL traffic (TCP/UDP/DNS) through Tor using Linux network namespaces. Unlike proxychains, this works with Go/Rust/static binarie… |
| `http2-header-impersonation` | recon | 9691 | Spoof HTTP/2 SETTINGS frames and pseudo-header order per browser profile. |
| `gitlab-public-recon` | recon | 9660 | Mine GitLab for secrets, CI tokens when subdomain found. |
| `iot-camera-recon` | recon | 9393 | Attack cameras via RTSP, ONVIF, Axis config when 554 open. |
| `wp-plugin-automation` | redteam | 9238 | Scripts and workflows to batch-test popular WordPress plugin CVEs across hundreds of domains. Covers automated plugin detection, version extraction from readme.txt, CVE matching ag… |
| `scada-hikvision-isapi` | recon | 9121 | Enumerate Hikvision ISAPI endpoints on SCADA and IoT web interfaces. |
| `humanize-automation` | recon | 9041 | Human-like mouse, keyboard and scroll behavior for behavioral bot bypass. |
| `hunt-deserialization` | redteam | 8997 | Hunt Insecure Deserialization — Java gadget chains (ysoserial), PHP object injection (phpggc), Python pickle RCE, .NET BinaryFormatter, Ruby Marshal.load, JNDI/Log4Shell. RCE via d… |
| `port-service-discovery` | recon | 8967 | Nmap scan for MySQL, Redis, FTP, SSH, internal API services. |
| `hunt-mcp-security` | redteam | 8951 | Hunt Model Context Protocol (MCP) vulnerabilities in AI-tool integration systems. |
| `zimbra-attack` | recon | 8950 | Zimbra SOAP user enum, CVE-2022-37042, SSRF when webmail. |
| `source-leak-hunt` | recon | 8893 | Mass scan for exposed env files, backups, and git configs. |
| `pentest-playbook` | meta | 8807 | 7-phase pentest pipeline from passive recon to exploitation. |
| `stealth-browser-launch` | recon | 8662 | Launch stealth Chromium with C++ fingerprint patches for anti-bot bypass. |
| `tls-fingerprint-impersonation` | recon | 8511 | Spoof TLS ClientHello and JA4 fingerprints for browser impersonation. |
| `api-noauth-hunt` | recon | 8199 | Use when an API may expose data or privileged operations without authentication. |
| `phpinfo-to-rce` | recon | 8194 | Chain phpinfo to RCE via exec check when info.php exposed. |
| `hunt-nosqli` | redteam | 8188 | Hunt NoSQL Injection — MongoDB operator injection ($where, $regex, $gt, $ne), CouchDB, Redis command injection, auth bypass via NoSQLi, data dump. Use when target uses MongoDB/Mong… |
| `cms-detection` | recon | 8040 | Identify CMS, frameworks, and server technology stacks on live hosts. |
| `wp-plugin-rest-auth-bypass` | recon | 7983 | Scan WordPress REST API plugin endpoints for unauthenticated state-changing operations — discover write endpoints (POST/PUT/PATCH/DELETE) exposed without auth, enumerate all plugin… |
| `hunt-wordpress` | redteam | 7977 | Use when an authorized target exposes WordPress core, plugin, theme, REST, or XML-RPC behavior. |
| `s3-minio-content-type-xss` | recon | 7542 | Exploit public bucket Content-Type override for stored XSS on target origin. |
| `hunt-nestjs` | redteam | 7518 | Hunt NestJS-specific vulnerabilities: guard bypass, decorator gaps, and microservice auth drift. |
| `hunt-information-disclosure` | redteam | 7504 | Hunt error leakage, DVCS exposure, source maps, config files, and differential oracles. |
| `recon-playbook` | meta | 7503 | Use when starting or restructuring an authorized external web and API assessment. |
| `port-mass-scan` | recon | 7421 | Port scan /8-/24 with Masscan+RustScan and nmap banners. |
| `github-secret-hunting` | recon | 7387 | Find leaked API keys, tokens, and credentials in public GitHub repositories. |
| `docker-privesc` | infra | 7259 | Escape Docker containers to host root via 5 techniques. |
| `unauth-api-flow-hijack` | recon | 7234 | Exploit unauthenticated multi-step API flows without credentials. |
| `hardcoded-credential-hunt` | recon | 7187 | Detect hardcoded passwords in HTML forms, JavaScript, and API responses. |
| `flask-werkzeug-attack` | recon | 7100 | Exploit Flask/Werkzeug debugger exposure for traceback and SECRET leaks. |
| `recon-sector` | redteam | 7062 | Parameterized sector recon using sector database. |
| `hunt-mass-assignment` | redteam | 6800 | Hunt mass assignment via sensitive field injection and ORM framework exploitation. |
| `origin-ip-discovery` | recon | 6770 | Discover origin IPs behind CDN/WAF via favicon hash, DNS history, and SSL certs. |
| `hunt-fastapi` | redteam | 6722 | Hunt FastAPI-specific vulnerabilities: dependency injection gaps, Pydantic coercion, and OpenAPI mining. |
| `cors-credential-wordpress` | recon | 6707 | Exploit WP CORS credential reflection for data theft. |
| `asn-infrastructure-mapping` | recon | 6555 | Map organization IP infrastructure via ASN, CIDR, TLD expansion, and reverse DNS. |
| `hunt-prototype-pollution` | redteam | 6373 | Hunt client-side and server-side prototype pollution for XSS, auth bypass, and RCE. |
| `visual-recon` | recon | 6213 | Screenshot all live hosts for rapid visual triage and technology fingerprinting. |
| `hunt-broken-function-level-auth` | redteam | 6184 | Hunt broken function-level authorization via verb drift, route shadowing, and transport gaps. |
| `hunt-schema-enumeration` | redteam | 5963 | Enumerate hidden tables, fields, and endpoints via API error hints. Agnostic across PostgREST, Zod, FastAPI, GraphQL, and REST. |
| `subdomain-takeover-hunt` | recon | 5900 | Detect and verify subdomain takeover via dangling CNAME to unclaimed services. |
| `hunt-django` | redteam | 5882 | Hunt Django-specific vulnerabilities: DRF permission gaps, ORM injection, and admin exploitation. |
| `hunt-write-gap` | redteam | 5876 | Hunt read-protected write-gaping endpoints. PATCH/POST/DELETE without authorization while GET is protected. Agnostic: Supabase, Firebase, REST, GraphQL. |
| `attack-patterns-reference` | meta | 5737 | Use when classifying a verified web or WordPress behavior and selecting a related validation skill. |
| `vhost-enumeration` | recon | 5694 | Discover hidden virtual hosts via Host header fuzzing and SSL certificate parsing. |
| `hunt-metrics-exposure` | redteam | 5470 | Hunt public /metrics, /health, and actuator endpoints leaking AI usage, DB pools, and operational intelligence. |
| `wordpress-cors-xmlrpc-rce-chain` | redteam | 5447 | Use when verified WordPress CORS, XML-RPC, role, upload, and execution behaviors may form one authorized attack path. |
| `cross-wave-delta-analysis` | meta | 5116 | Compare recon waves to find NEW, REGRESSED, PERSISTENT findings. |
| `wordpress-full-compromise` | chains | 4815 | Use when verified WordPress findings may combine into an authorized path to administrative or server control. |
| `cross-attack-chains` | chains | 4793 | Use when two or more verified findings may combine into a higher-impact authorized attack path. |
| `google-dorks-catalog` | meta | 4224 | High-precision Google dorks for exposed configs, secrets, and credentials -- real-world validated |
| `email-security` | recon | 3187 | DMARC/SPF/DKIM check, email spoofing, SMTP test, and security header analysis |

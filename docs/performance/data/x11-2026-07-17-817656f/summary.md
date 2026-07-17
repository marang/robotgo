# X11 native CGO vs Pure-Go benchmark report

> Report-only evidence: correctness is blocking; timing ratios never fail CI. A 1x CI smoke validates this measurement path only and is not decision-grade performance data.

- Git commit: `817656f2d52140581f7f6c5535d86f050ee6663b`
- Source fingerprint: `c982022e8255b2a1773e452b5c45a7c0a0aa98da`
- Observations per benchmark and implementation: `10`
- Go benchtime: `500ms`
- GOMAXPROCS / `-test.cpu`: `1`
- Benchmark cycles: `5`
- Balanced two-order mode: `enabled`

| Benchmark | Metric | native CGO median [Q1–Q3] | Pure-Go median [Q1–Q3] | Pure-Go / native CGO | N (native CGO / Pure-Go) |
|---|---:|---:|---:|---:|---:|
| BenchmarkCaptureImgRuntime | ns/op | 998830.5 [987831.5–1.0059575e+06] | 3.416314e+06 [3.38685475e+06–3.4424345e+06] | 3.420x | 10 / 10 |
| BenchmarkCaptureImgRuntime | B/op | 1.228868e+06 [1.228864e+06–1.22887275e+06] | 1.342656e+06 [1.342656e+06–1.342656e+06] | 1.093x | 10 / 10 |
| BenchmarkCaptureImgRuntime | allocs/op | 2 [2–2] | 116 [116–116] | 58.000x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | ns/op | 103533 [102332.25–104034.75] | 525753.5 [524579.75–530101] | 5.078x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | B/op | 207 [207–208] | 4192 [4192–4192] | 20.251x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | allocs/op | 8 [8–9] | 95 [95–95] | 11.875x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | ns/op | 5.438876e+06 [5.41949075e+06–5.44573325e+06] | 6.0131985e+06 [5.99629875e+06–6.03745325e+06] | 1.106x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | B/op | 184 [183.25–184] | 3184 [3184–3184] | 17.304x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | allocs/op | 7 [6.25–7] | 73 [73–73] | 10.429x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | ns/op | 3.46283e+06 [3.445541e+06–3.4694925e+06] | 7.130198e+06 [7.0348915e+06–7.2446715e+06] | 2.059x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | B/op | 184 [184–184] | 59411 [59411–59411] | 322.886x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | allocs/op | 7 [7–7] | 156 [156–156] | 22.286x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | ns/op | 152907 [151231–153296.5] | 1.5571075e+06 [1.4746515e+06–1.569438e+06] | 10.183x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | B/op | 208 [208–208] | 60419 [60419–60419] | 290.476x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | allocs/op | 9 [9–9] | 178 [178–178] | 19.778x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | ns/op | 13898.5 [13644–14155.25] | 77948 [76985–79315] | 5.608x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | B/op | 0 [0–0] | 896 [896–896] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/Location | allocs/op | 0 [0–0] | 22 [22–22] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | ns/op | 34763 [34619–34962.75] | 83429.5 [81637.75–84581.75] | 2.400x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | B/op | 87 [87–87] | 640 [640–640] | 7.356x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | allocs/op | 3 [3–3] | 14 [14–14] | 4.667x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | ns/op | 51805.5 [51391.75–51992] | 83012 [81606.75–83497.75] | 1.602x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | B/op | 87.5 [87–88] | 624 [624–624] | 7.131x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | allocs/op | 3 [3–4] | 14 [14–14] | 4.667x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | ns/op | 44199 [43682.75–44911.5] | 389881 [384723.25–396420] | 8.821x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | B/op | 182 [180.5–182.75] | 3184 [3184–3184] | 17.495x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | allocs/op | 6 [6–6] | 73 [73–73] | 12.167x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | ns/op | 4.44922975e+07 [4.434495225e+07–4.4646628e+07] | 5.9201137e+07 [5.86767015e+07–5.94644065e+07] | 1.331x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | B/op | 2040 [2040–2040] | 540368 [540368–540368] | 264.886x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | allocs/op | 81 [81–81] | 1527 [1527–1527] | 18.852x | 10 / 10 |

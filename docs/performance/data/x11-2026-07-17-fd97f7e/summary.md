# X11 native CGO vs Pure-Go benchmark report

> Report-only evidence: correctness is blocking; timing ratios never fail CI. A 1x CI smoke validates this measurement path only and is not decision-grade performance data.

- Git commit: `fd97f7e61379feaa554fd62a1f00a88553dad9bb`
- Source fingerprint: `673b9be744c4a3263a4070bbbb1487a7b6e9d533`
- Observations per benchmark and implementation: `10`
- Go benchtime: `500ms`
- GOMAXPROCS / `-test.cpu`: `1`
- Benchmark cycles: `5`
- Balanced two-order mode: `enabled`

| Benchmark | Metric | native CGO median [Q1–Q3] | Pure-Go median [Q1–Q3] | Pure-Go / native CGO | N (native CGO / Pure-Go) |
|---|---:|---:|---:|---:|---:|
| BenchmarkCaptureImgRuntime | ns/op | 1.0911985e+06 [1.0692335e+06–1.13975925e+06] | 3.73411e+06 [3.646454e+06–3.80974175e+06] | 3.422x | 10 / 10 |
| BenchmarkCaptureImgRuntime | B/op | 1.228864e+06 [1.228864e+06–1.22887375e+06] | 1.342656e+06 [1.342656e+06–1.342656e+06] | 1.093x | 10 / 10 |
| BenchmarkCaptureImgRuntime | allocs/op | 2 [2–2] | 116 [116–116] | 58.000x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | ns/op | 108941.5 [108013.75–111219.75] | 595001.5 [592038.75–597795.25] | 5.462x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | B/op | 207.5 [207–208] | 4192 [4192–4192] | 20.202x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | allocs/op | 8.5 [8–9] | 95 [95–95] | 11.176x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | ns/op | 5.366506e+06 [5.34906025e+06–5.396991e+06] | 5.891172e+06 [5.8735675e+06–5.91304725e+06] | 1.098x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | B/op | 184 [183.25–184] | 2864 [2864–2864] | 15.565x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | allocs/op | 7 [6.25–7] | 63 [63–63] | 9.000x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | ns/op | 3.4182045e+06 [3.404776e+06–3.42637825e+06] | 7.0435395e+06 [7.03428075e+06–7.08573175e+06] | 2.061x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | B/op | 184 [184–184] | 59043 [59043–59043] | 320.886x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | allocs/op | 7 [7–7] | 146 [146–146] | 20.857x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | ns/op | 161805.5 [161084.75–166672.25] | 1.7107545e+06 [1.6873405e+06–1.7838675e+06] | 10.573x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | B/op | 208 [208–208] | 60419 [60419–60419] | 290.476x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | allocs/op | 9 [9–9] | 178 [178–178] | 19.778x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | ns/op | 14980 [14912–15391.25] | 86818.5 [85822.75–87501.75] | 5.796x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | B/op | 0 [0–0] | 896 [896–896] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/Location | allocs/op | 0 [0–0] | 22 [22–22] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | ns/op | 37962 [37738.75–38072.75] | 92905 [92029.75–94121.5] | 2.447x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | B/op | 87 [86–87] | 640 [640–640] | 7.356x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | allocs/op | 3 [3–3] | 14 [14–14] | 4.667x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | ns/op | 56747.5 [56393.75–57818.25] | 92192.5 [91658.5–94463.75] | 1.625x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | B/op | 87 [87–88] | 624 [624–624] | 7.172x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | allocs/op | 3 [3–4] | 14 [14–14] | 4.667x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | ns/op | 47967.5 [46925.5–49705.5] | 384124 [381878.5–398405.5] | 8.008x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | B/op | 181.5 [181–183] | 2832 [2832–2832] | 15.603x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | allocs/op | 6 [6–6] | 63 [63–63] | 10.500x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | ns/op | 4.3937117e+07 [4.38917345e+07–4.401909625e+07] | 5.7852657e+07 [5.77290835e+07–5.85946305e+07] | 1.317x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | B/op | 2040 [2040–2040] | 537456 [537456–537456] | 263.459x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | allocs/op | 81 [81–81] | 1447 [1447–1447] | 17.864x | 10 / 10 |

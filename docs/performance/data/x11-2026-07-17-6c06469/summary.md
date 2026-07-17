# X11 native CGO vs Pure-Go benchmark report

> Report-only evidence: correctness is blocking; timing ratios never fail CI. A 1x CI smoke validates this measurement path only and is not decision-grade performance data.

- Git commit: `6c064690be8ba6e8e57934298f394f224aba30a9`
- Source fingerprint: `9b9cba4599fa1b01d114a5a77d1dfcd6d96f48e5`
- Observations per benchmark and implementation: `10`
- Go benchtime: `500ms`
- GOMAXPROCS / `-test.cpu`: `1`
- Benchmark cycles: `5`
- Balanced two-order mode: `enabled`

| Benchmark | Metric | native CGO median [Q1–Q3] | Pure-Go median [Q1–Q3] | Pure-Go / native CGO | N (native CGO / Pure-Go) |
|---|---:|---:|---:|---:|---:|
| BenchmarkCaptureImgRuntime | ns/op | 975403 [960025.75–999197.75] | 3.377041e+06 [3.299047e+06–3.473225e+06] | 3.462x | 10 / 10 |
| BenchmarkCaptureImgRuntime | B/op | 1.228872e+06 [1.228866e+06–1.228873e+06] | 1.342656e+06 [1.342656e+06–1.342656e+06] | 1.093x | 10 / 10 |
| BenchmarkCaptureImgRuntime | allocs/op | 2 [2–2] | 116 [116–116] | 58.000x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | ns/op | 100500 [98677.5–101247.75] | 539108.5 [518472.25–553776.25] | 5.364x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | B/op | 207 [207–208] | 8361 [8361–8361] | 40.391x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | allocs/op | 8 [8–9] | 140 [140–140] | 17.500x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | ns/op | 5.4542945e+06 [5.42062525e+06–5.47600625e+06] | 6.0062685e+06 [5.9775375e+06–6.02335625e+06] | 1.101x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | B/op | 184 [184–184] | 6216 [6216–6216] | 33.783x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | allocs/op | 7 [7–7] | 106 [106–106] | 15.143x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | ns/op | 3.468379e+06 [3.4556255e+06–3.47412525e+06] | 7.1865255e+06 [7.11118075e+06–7.2641675e+06] | 2.072x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | B/op | 184 [184–184] | 73679 [73678–73679] | 400.429x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | allocs/op | 7 [7–7] | 207 [207–207] | 29.571x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | ns/op | 151860 [149708.5–154182.5] | 1.666809e+06 [1.56921175e+06–1.71647575e+06] | 10.976x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | B/op | 208 [208–208] | 75824.5 [75824–75825] | 364.541x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | allocs/op | 9 [9–9] | 241 [241–241] | 26.778x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | ns/op | 13892.5 [13675.75–14050.5] | 79661 [77901.75–80733] | 5.734x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | B/op | 0 [0–0] | 1528 [1528–1528] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/Location | allocs/op | 0 [0–0] | 29 [29–29] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | ns/op | 34302 [33875.75–34496.5] | 82373 [80728–86175] | 2.401x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | B/op | 87 [87–87] | 1272 [1272–1272] | 14.621x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | allocs/op | 3 [3–3] | 21 [21–21] | 7.000x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | ns/op | 50425 [50236.5–51025.5] | 82293 [80512–85439.25] | 1.632x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | B/op | 87 [86.25–87.75] | 1248 [1248–1248] | 14.345x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | allocs/op | 3 [3–3.75] | 21 [21–21] | 7.000x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | ns/op | 41829.5 [41446–42227.5] | 395413 [386277.5–401236.75] | 9.453x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | B/op | 183 [182–183] | 6216 [6216–6216] | 33.967x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | allocs/op | 6 [6–6] | 106 [106–106] | 17.667x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | ns/op | 4.4458198e+07 [4.43448655e+07–4.4773492e+07] | 5.94151635e+07 [5.923309025e+07–6.099906475e+07] | 1.336x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | B/op | 2040 [2040–2040] | 672568 [672568–672568] | 329.690x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | allocs/op | 81 [81–81] | 2020 [2020–2020] | 24.938x | 10 / 10 |

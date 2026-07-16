# X11 native CGO vs Pure-Go benchmark report

> Report-only evidence: correctness is blocking; timing ratios never fail CI. A 1x CI smoke validates this measurement path only and is not decision-grade performance data.

- Git commit: `d5fd51c72702a719fd60fb06e0ef246018dc8b4e`
- Source fingerprint: `c704c843b5bf4b2422f162a330bbac641f524e94`
- Observations per benchmark and implementation: `10`
- Go benchtime: `500ms`
- GOMAXPROCS / `-test.cpu`: `1`
- Balanced two-order cycles: `1`

| Benchmark | Metric | native CGO median [Q1–Q3] | Pure-Go median [Q1–Q3] | Pure-Go / native CGO | N (native CGO / Pure-Go) |
|---|---:|---:|---:|---:|---:|
| BenchmarkCaptureImgRuntime | ns/op | 1.0015875e+06 [995235.25–1.010083e+06] | 3.49904e+06 [3.43283975e+06–3.66112725e+06] | 3.493x | 10 / 10 |
| BenchmarkCaptureImgRuntime | B/op | 1.228864e+06 [1.228864e+06–1.228864e+06] | 1.331208e+06 [1.331207e+06–1.331208e+06] | 1.083x | 10 / 10 |
| BenchmarkCaptureImgRuntime | allocs/op | 2 [2–2] | 115 [115–115] | 57.500x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | ns/op | 92519 [91361.75–94857.5] | 129050 [126892.75–134585.75] | 1.395x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | B/op | 205 [204.25–205.75] | 6648 [6648–6648] | 32.429x | 10 / 10 |
| BenchmarkX11InputRuntime/ButtonTogglePair | allocs/op | 8 [8–8] | 111 [111–111] | 13.875x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | ns/op | 5.4897935e+06 [5.469016e+06–5.49926075e+06] | 5.577592e+06 [5.5411895e+06–5.59022575e+06] | 1.016x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | B/op | 184.5 [184–185] | 4696 [4696–4696] | 25.453x | 10 / 10 |
| BenchmarkX11InputRuntime/ClickLeft | allocs/op | 7 [7–7] | 79 [79–79] | 11.286x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | ns/op | 3.5094465e+06 [3.49190475e+06–3.519565e+06] | 5.7098935e+06 [5.68766675e+06–5.7411165e+06] | 1.627x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | B/op | 184 [184–215.75] | 39512 [39512–39512] | 214.739x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyPressEnter | allocs/op | 7 [7–7] | 120 [120–120] | 17.143x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | ns/op | 139518 [138524.5–141704] | 187904.5 [185308.25–194256] | 1.347x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | B/op | 237 [237–237] | 41496 [41496–41496] | 175.089x | 10 / 10 |
| BenchmarkX11InputRuntime/KeyTogglePairEnter | allocs/op | 10 [10–10] | 154 [154–154] | 15.400x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | ns/op | 14867 [14743.75–15158.25] | 15108.5 [14899.75–16152.5] | 1.016x | 10 / 10 |
| BenchmarkX11InputRuntime/Location | B/op | 0 [0–0] | 544 [544–544] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/Location | allocs/op | 0 [0–0] | 10 [10–10] | n/a | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | ns/op | 37223 [36728–37638] | 21041 [20632.25–21633.75] | 0.565x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | B/op | 87 [86.25–87] | 1084 [1084–1084] | 12.460x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveAbsolute | allocs/op | 3 [3–3] | 18 [18–18] | 6.000x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | ns/op | 51655.5 [50710–52470] | 20584 [20246.25–20830.5] | 0.398x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | B/op | 87 [86.25–87] | 1084 [1084–1084] | 12.460x | 10 / 10 |
| BenchmarkX11InputRuntime/MoveRelative | allocs/op | 3 [3–3] | 18 [17–18] | 6.000x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollHorizontal1 | ns/op | 45496 [44894–45935.5] | 91768 [90834–94955.25] | 2.017x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollHorizontal1 | B/op | 181 [181–182.75] | 4696 [4696–4696] | 25.945x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollHorizontal1 | allocs/op | 6 [6–6] | 79 [79–79] | 13.167x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | ns/op | 44982.5 [44705.5–45596.75] | 93720.5 [93122–96659.75] | 2.083x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | B/op | 181 [180–182] | 4696 [4696–4696] | 25.945x | 10 / 10 |
| BenchmarkX11InputRuntime/ScrollVertical1 | allocs/op | 6 [6–6] | 79 [79–79] | 13.167x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | ns/op | 4.51843195e+07 [4.507439725e+07–4.5279859e+07] | 4.57053865e+07 [4.542133375e+07–4.605588e+07] | 1.012x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | B/op | 2040 [2040–2040] | 358329 [358329–358329] | 175.651x | 10 / 10 |
| BenchmarkX11InputRuntime/TypeASCII8 | allocs/op | 81 [81–81] | 1138 [1138–1138] | 14.049x | 10 / 10 |

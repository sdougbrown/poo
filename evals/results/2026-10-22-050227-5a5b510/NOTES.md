# Stage 4 eval gate notes

The comparison flags one regression: `minimax-m3.1-flash` success 0.917 against 1.0. Four of its five failures settled `provider_error` with `provider:network` ("http2: timeout awaiting response headers" from ollama.com) between 05:08 and 05:09 UTC: an endpoint outage, not a poo failure. The fifth (`fix-path-join-double-slash` seed 2) settled `output_truncated`.

The two affected tasks were rerun for this model with the same binary (`rerun-minimax/`): 6 of 6 succeeded. Without the network failures the model scores 59 of 60.

#!/bin/bash
set -x
PREDICTIONS_PATH=$1
RUN_ID=$(date +"%Y%m%d_%H%M%S")
python -m swebench.harness.run_evaluation \
        --dataset_name "princeton-nlp/SWE-bench_Verified" \
        --split "test" \
        --predictions_path $PREDICTIONS_PATH \
        --timeout 3600 \
        --max_workers 5 \
        --run_id $RUN_ID
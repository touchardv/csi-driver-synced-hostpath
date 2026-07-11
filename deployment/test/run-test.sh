#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
WRITER_MANIFEST="${SCRIPT_DIR}/writer.yaml"
READER_MANIFEST="${SCRIPT_DIR}/reader.yaml"
NAMESPACE="default"

echo "=== 1. Cleaning up any leftover test resources ==="
kubectl delete -f "$WRITER_MANIFEST" -f "$READER_MANIFEST" --namespace "$NAMESPACE" --ignore-not-found=true

echo "=== 2. Creating PVC and Writer Job ==="
kubectl apply -f "$WRITER_MANIFEST" --namespace "$NAMESPACE"

echo "=== 3. Waiting for Writer Job to complete ==="
kubectl wait --namespace "$NAMESPACE" --for=condition=complete job/csi-test-writer --timeout=120s

# Get the node name where the writer pod executed
WRITER_NODE=$(kubectl get pods --namespace "$NAMESPACE" -l test-role=writer -o jsonpath='{.items[0].spec.nodeName}')
echo "Writer ran on node: ${WRITER_NODE}"

echo "=== 4. Deleting Writer Job to trigger Volume Upload ==="
kubectl delete job csi-test-writer --namespace "$NAMESPACE"
kubectl wait --namespace "$NAMESPACE" --for=delete pod -l test-role=writer --timeout=120s

# Find another node in the cluster
READER_NODE=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | grep -v "^${WRITER_NODE}$" | head -n 1 || true)

if [ -n "$READER_NODE" ]; then
    echo "Directing Reader Job to run on a different node: ${READER_NODE}"
    # Inject nodeName under spec.template.spec in reader.yaml
    UPDATED_READER_MANIFEST=$(mktemp)
    awk -v node="$READER_NODE" '
      /^    spec:/ {
        print $0;
        print "      nodeName: " node;
        next;
      }
      { print }
    ' "$READER_MANIFEST" > "$UPDATED_READER_MANIFEST"
else
    echo "⚠️ Only one node detected or other nodes unavailable. Reader will run on the same node."
    UPDATED_READER_MANIFEST="$READER_MANIFEST"
fi

echo "=== 5. Starting Reader Job ==="
kubectl apply -f "$UPDATED_READER_MANIFEST" --namespace "$NAMESPACE"

echo "=== 6. Waiting for Reader Job to complete ==="
if ! kubectl wait --namespace "$NAMESPACE" --for=condition=complete job/csi-test-reader --timeout=120s; then
    echo "❌ Test failed: Reader Job did not complete successfully."
    echo "--- Reader Job Logs ---"
    kubectl logs -n "$NAMESPACE" -l test-role=reader --tail=-1
    # Cleanup temp file if created
    [ "$UPDATED_READER_MANIFEST" != "$READER_MANIFEST" ] && rm -f "$UPDATED_READER_MANIFEST"
    exit 1
fi

echo "=== 7. Validating Data Read ==="
OUTPUT=$(kubectl logs -n "$NAMESPACE" -l test-role=reader --tail=-1)
echo "Reader output:"
echo "$OUTPUT"

# Cleanup temp file if created
[ "$UPDATED_READER_MANIFEST" != "$READER_MANIFEST" ] && rm -f "$UPDATED_READER_MANIFEST"

if echo "$OUTPUT" | grep -q "CSI_INTEGRATION_TEST_PASSED"; then
    echo "==================================="
    echo "🎉 SUCCESS: CSI Sync Integration Test Passed!"
    echo "==================================="
else
    echo "❌ Test failed: Output did not contain expected content."
    exit 1
fi

echo "=== 8. Cleaning up test resources ==="
kubectl delete -f "$WRITER_MANIFEST" -f "$READER_MANIFEST" --namespace "$NAMESPACE" --ignore-not-found=true

#!/usr/bin/env bash
# Tenant Isolation Test
# Verifies that tenant A cannot access tenant B's data.
#
# Requires: gateway on :8080, auth on :3456
set -euo pipefail

AUTH_URL="http://localhost:3456"
GW_URL="http://localhost:8080"
PASS="testpass12345"

echo "=== Tenant Isolation Test ==="

# Helper: sign up and get a JWT
signup_and_token() {
  local email="$1"
  local name="$2"
  local cookie
  cookie=$(curl -s -X POST "$AUTH_URL/api/auth/sign-up/email" \
    -H "Content-Type: application/json" -H "Origin: http://localhost:5174" \
    -d "{\"email\":\"$email\",\"password\":\"$PASS\",\"name\":\"$name\"}" \
    -D /dev/stderr 2>&1 1>/dev/null | grep -i "set-cookie" | sed 's/.*better-auth.session_token=//;s/;.*//')
  curl -s "$AUTH_URL/api/auth/token" -H "Cookie: better-auth.session_token=$cookie" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])"
}

# Create two users in separate tenants
echo "1. Creating tenant-A user..."
TOKEN_A=$(signup_and_token "isolation-a-$$@test.com" "Tenant A User")

echo "2. Creating tenant-B user..."
TOKEN_B=$(signup_and_token "isolation-b-$$@test.com" "Tenant B User")

# Get tenant IDs
TENANT_A=$(curl -s "$GW_URL/v1/auth/me" -H "Authorization: Bearer $TOKEN_A" | python3 -c "import sys,json; print(json.load(sys.stdin)['tenant_id'])")
TENANT_B=$(curl -s "$GW_URL/v1/auth/me" -H "Authorization: Bearer $TOKEN_B" | python3 -c "import sys,json; print(json.load(sys.stdin)['tenant_id'])")
echo "   Tenant A: $TENANT_A"
echo "   Tenant B: $TENANT_B"

# User A creates a conversation
echo "3. User A creates a conversation..."
CONV_A=$(curl -s -X POST "$GW_URL/v1/conversations" \
  -H "Authorization: Bearer $TOKEN_A" -H "Content-Type: application/json" \
  -d '{"title":"Secret A Conversation"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "   Conv ID: $CONV_A"

# User B should NOT see User A's conversation
echo "4. Checking User B cannot see User A's conversations..."
B_CONVS=$(curl -s "$GW_URL/v1/conversations" -H "Authorization: Bearer $TOKEN_B" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('conversations',[])))")
if [ "$B_CONVS" != "0" ]; then
  echo "   FAIL: User B sees $B_CONVS conversations (expected 0)"
  exit 1
fi
echo "   PASS: User B sees 0 conversations"

# User B should NOT access User A's conversation directly
echo "5. Checking User B cannot access User A's conversation by ID..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$GW_URL/v1/conversations/$CONV_A" -H "Authorization: Bearer $TOKEN_B")
if [ "$STATUS" == "200" ]; then
  echo "   FAIL: User B got 200 accessing tenant A's conversation"
  exit 1
fi
echo "   PASS: User B got $STATUS (expected 403 or 404)"

# User A should see their own tenants only (unless platform_admin)
echo "6. Checking User A only sees their own tenant..."
A_TENANTS=$(curl -s "$GW_URL/v1/tenants" -H "Authorization: Bearer $TOKEN_A" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('tenants',[])))")
if [ "$A_TENANTS" != "1" ]; then
  echo "   FAIL: User A sees $A_TENANTS tenants (expected 1)"
  exit 1
fi
echo "   PASS: User A sees 1 tenant"

# Non-admin should NOT access admin endpoints
echo "7. Checking non-admin rejected from admin endpoints..."
ADMIN_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$GW_URL/v1/admin/llm/providers" -H "Authorization: Bearer $TOKEN_A")
if [ "$ADMIN_STATUS" == "200" ]; then
  echo "   FAIL: Non-admin got 200 on admin endpoint"
  exit 1
fi
echo "   PASS: Non-admin got $ADMIN_STATUS"

echo ""
echo "=== ALL TESTS PASSED ==="

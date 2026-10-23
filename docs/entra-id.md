# Microsoft Entra ID (Microsoft 365) login

This guide connects chronicle to Microsoft Entra ID. Users log in with their Microsoft 365 account. Each team sees only its own namespaces.

The end-to-end tests use a mock of Entra ID and Microsoft Graph (`test/mockentra`), with group overage and two teams. The names in the Entra admin center can differ from the steps below when Microsoft changes the portal.

You need:

- An Entra role that can register applications (for example "Application Developer").
- The tenant ID of your organization.
- The URL of chronicle, for example `https://chronicle.example.com`.

## 1. Register the application

1. Open the Microsoft Entra admin center. Go to **Identity > Applications > App registrations**.
2. Select **New registration**.
3. Type the name `chronicle`.
4. For **Supported account types**, select **Accounts in this organizational directory only (single tenant)**.
5. For **Redirect URI**, select **Web**. Type `https://chronicle.example.com/auth/callback`.
6. Select **Register**.
7. Write down the **Application (client) ID** and the **Directory (tenant) ID**.

chronicle accepts only your tenant. It does not use the `common` endpoint, and it rejects tokens with another `tid`.

## 2. Make a client secret

1. In the app registration, go to **Certificates & secrets**.
2. Select **New client secret**. Set an expiry date that your security policy permits.
3. Copy the secret **Value**. Entra shows it only one time.
4. Put the value in a Kubernetes Secret in the namespace of chronicle:

```sh
kubectl -n chronicle-system create secret generic chronicle-entra --from-literal=client-secret='<value>'
```

Do not put the secret in a ConfigMap or in Git.

## 3. Send the groups in the token

1. In the app registration, go to **Token configuration**.
2. Select **Add groups claim**.
3. Select **Security groups**.
4. For the **ID** token type, select **Group ID**. Select **Add**.

The token then has the claim `groups` with the object IDs of the security groups of the user.

When a user is in more than 200 groups, Entra does not put the groups in the token. It sends `_claim_names` instead ("group overage"). Then chronicle reads the groups from Microsoft Graph with the access token of the user. For this:

1. Go to **API permissions**. Make sure that **Microsoft Graph > User.Read** (delegated) is in the list. It is there by default.
2. If your tenant asks for admin consent, select **Grant admin consent**.

chronicle keeps the groups from Microsoft Graph for 10 minutes.

To use app roles instead of groups, define the roles in **App roles**. Assign users or groups to them. Then set `groupsFrom: roles` (step 4).

## 4. Configure chronicle

Use these Helm values:

```yaml
auth:
  entra:
    tenantID: 11111111-2222-3333-4444-555555555555
    clientID: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
    clientSecret:
      existingSecret: chronicle-entra
      key: client-secret
    # groupsFrom: roles
  redirectURL: https://chronicle.example.com/auth/callback
```

chronicle maps each user and group to a Kubernetes name with a prefix:

| Entra | Kubernetes |
|---|---|
| User with object ID `9f1c...` | User `entra:9f1c...` |
| Security group with object ID `3b7e...` | Group `entra:group:3b7e...` |
| App role `Operator` (with `groupsFrom: roles`) | Group `entra:role:Operator` |

The prefixes make sure that no Entra name can be a Kubernetes system name.

## 5. Give each team its namespaces

A team is a set of namespaces. The Helm value `teams` creates the RoleBindings. Each Entra group gets one of the roles `viewer`, `editor` or `admin` in the namespaces of its team:

```yaml
teams:
  - name: data
    namespaces: [data-dev, data-prod]
    viewer: [3b7e0000-0000-0000-0000-000000000001]   # Data readers
    editor: [3b7e0000-0000-0000-0000-000000000002]   # Data engineers
  - name: web
    namespaces: [web]
    admin: [3b7e0000-0000-0000-0000-000000000003]    # Web platform team
```

| Role | Permissions |
|---|---|
| `viewer` | See runs, templates, cron workflows and logs. |
| `editor` | The viewer permissions, and submit, retry, re-run, suspend, resume, stop and approve runs. Change templates and cron workflows. |
| `admin` | The editor permissions, and delete runs and give permissions in the namespace. |

The UI and the API show a user only the namespaces where the user can list runs. The same RBAC applies to `kubectl`, GitOps and the chronicle CLI.

## 6. Test the login

1. Open `https://chronicle.example.com`. Select **Log in with Microsoft**.
2. Log in with a user of a team.
3. Make sure that the namespace list shows only the namespaces of the team.
4. Select your name at the top right. The menu shows your Kubernetes user and groups. Use them to examine RBAC problems with `kubectl auth can-i --as entra:<oid> --as-group entra:group:<id> list workflows -n <namespace>`.

## Problems

| Problem | Cause and fix |
|---|---|
| "the token is from tenant ..., not from the configured tenant" | The user is from another tenant (a guest account in its home tenant). Use an account of your tenant. |
| The user sees no namespace | The group is not in `teams`, or the token has no `groups` claim. Do step 3 again. |
| "the user has too many groups for a token: log in to the UI first" | The CLI sent a token with a group overage. Log in to the UI one time; chronicle then knows the groups for 10 minutes. |
| "read the groups from Microsoft Graph: status 403" | The app has no `User.Read` permission or no admin consent. Do step 3 again. |

import datetime
import json

from django.utils import timezone

from authentik.core.models import Application, Token, User
from authentik.flows.models import Flow
from authentik.providers.oauth2.models import AccessToken, OAuth2Provider


def ensure_user(username: str, email: str, password: str) -> User:
    user, _ = User.objects.get_or_create(
        username=username,
        defaults={"name": username, "email": email},
    )
    user.name = username
    user.email = email
    user.is_active = True
    user.set_password(password)
    user.save()
    return user


def ensure_api_token(admin: User) -> None:
    Token.objects.update_or_create(
        identifier="mailcloak-e2e-api",
        defaults={
            "intent": "api",
            "user": admin,
            "key": "mailcloak-authentik-api-token",
            "expiring": False,
            "description": "mailcloak e2e API token",
        },
    )


def ensure_provider() -> OAuth2Provider:
    auth_flow = Flow.objects.get(slug="default-authentication-flow")
    authz_flow = Flow.objects.get(slug="default-provider-authorization-implicit-consent")
    invalidation_flow = Flow.objects.get(slug="default-provider-invalidation-flow")

    provider, _ = OAuth2Provider.objects.update_or_create(
        name="mailcloak-e2e-oauth",
        defaults={
            "client_id": "mailcloak-admin",
            "client_secret": "mailcloak-admin-secret",
            "client_type": "confidential",
            "authentication_flow": auth_flow,
            "authorization_flow": authz_flow,
            "invalidation_flow": invalidation_flow,
            "access_code_validity": "minutes=5",
            "access_token_validity": "hours=1",
            "refresh_token_validity": "days=1",
            "sub_mode": "hashed_user_id",
            "issuer_mode": "global",
        },
    )
    return provider


def ensure_application(provider: OAuth2Provider) -> None:
    app, _ = Application.objects.get_or_create(
        slug="mailcloak-e2e-app",
        defaults={"name": "mailcloak-e2e-app", "provider": provider},
    )
    if app.provider_id != provider.provider_ptr_id:
        app.provider = provider
        app.save(update_fields=["provider"])


def ensure_access_token(provider: OAuth2Provider, user: User) -> None:
    token_value = f"{user.username}-direct-access-token"
    expiry = timezone.now() + datetime.timedelta(hours=1)
    id_token = json.dumps({"sub": user.username})

    AccessToken.objects.update_or_create(
        token=token_value,
        defaults={
            "provider": provider,
            "user": user,
            "revoked": False,
            "_scope": "openid profile email",
            "auth_time": timezone.now(),
            "expires": expiry,
            "expiring": True,
            "_id_token": id_token,
        },
    )


alice = ensure_user("alice", "alice@d1.test", "password")
bob = ensure_user("bob", "bob@d2.test", "password")
admin = User.objects.filter(username="akadmin").first()
if admin is None:
    raise RuntimeError("bootstrap admin user akadmin not found")

ensure_api_token(admin)
provider = ensure_provider()
ensure_application(provider)
ensure_access_token(provider, alice)
ensure_access_token(provider, bob)

print("AUTHENTIK_BOOTSTRAP_OK")

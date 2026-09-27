/* WebAuthn options and responses use the browser's native JSON codecs. */
(function () {
  async function request(url, payload, method = 'POST') {
    const res = await fetch(url, {
      method, credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: payload === undefined ? undefined : JSON.stringify(payload)
    });
    if (!res.ok) throw new Error(await res.text());
    return res.status === 204 || res.status === 201 ? null : res.json();
  }
  function supported() {
    if (!window.PublicKeyCredential ||
        !PublicKeyCredential.parseCreationOptionsFromJSON ||
        !PublicKeyCredential.parseRequestOptionsFromJSON) {
      throw new Error('This browser does not support WebAuthn JSON.');
    }
  }
  function failed(error) { alert(error.message || String(error)); }

  const register = document.getElementById('add-passkey');
  if (register) register.addEventListener('click', async function () {
    try {
      supported();
      const options = await request('/account/passkeys/begin', {});
      const credential = await navigator.credentials.create({
        publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey)
      });
      await request('/account/passkeys/finish', credential.toJSON());
      window.location.reload();
    } catch (error) { failed(error); }
  });

  document.querySelectorAll('[data-passkey-id]').forEach(button => {
    button.addEventListener('click', async function () {
      if (!confirm('Delete this passkey?')) return;
      try {
        await request('/account/passkeys/' + encodeURIComponent(button.dataset.passkeyId), undefined, 'DELETE');
        window.location.reload();
      } catch (error) { failed(error); }
    });
  });

  const login = document.getElementById('passkey-login');
  if (login) login.addEventListener('click', async function () {
    try {
      supported();
      const email = document.getElementById('loginEmail').value;
      if (!email) { document.getElementById('loginEmail').focus(); return; }
      const options = await request('/auth/passkey/login/begin', {email});
      const credential = await navigator.credentials.get({
        publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options.publicKey)
      });
      const response = await request('/auth/passkey/login/finish', {
        ...credential.toJSON(),
        return_to: document.querySelector('input[name="return_to"]')?.value || ''
      });
      if (response && response.redirect && response.redirect.startsWith('/') && !response.redirect.startsWith('//')) {
        window.location.assign(response.redirect);
      }
    } catch (error) { failed(error); }
  });
})();

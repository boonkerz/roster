import SwiftUI

struct LoginView: View {
    @EnvironmentObject var app: AppState
    @State private var server = ""
    @State private var username = ""
    @State private var password = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("Server") {
                    TextField("https://roster.example.com", text: $server)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                }
                Section("Anmeldung") {
                    TextField("Benutzername", text: $username)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    SecureField("Passwort", text: $password)
                }
                if let e = app.errorMessage {
                    Text(e).foregroundStyle(.red).font(.footnote)
                }
                Section {
                    Button {
                        app.serverURL = server
                        Task { await app.login(username: username, password: password) }
                    } label: {
                        HStack {
                            if app.busy { ProgressView() }
                            Text("Anmelden")
                        }
                    }
                    .disabled(app.busy || server.isEmpty || username.isEmpty || password.isEmpty)
                }
            }
            .navigationTitle("Roster")
            .onAppear { if server.isEmpty { server = app.serverURL } }
        }
    }
}

struct TOTPView: View {
    @EnvironmentObject var app: AppState
    @State private var code = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("Zwei-Faktor-Code") {
                    TextField("123456", text: $code)
                        .keyboardType(.numberPad)
                        .textContentType(.oneTimeCode)
                }
                if let e = app.errorMessage {
                    Text(e).foregroundStyle(.red).font(.footnote)
                }
                Section {
                    Button {
                        Task { await app.submitTOTP(code: code) }
                    } label: {
                        HStack {
                            if app.busy { ProgressView() }
                            Text("Bestätigen")
                        }
                    }
                    .disabled(app.busy || code.count < 6)
                    Button("Abbrechen", role: .cancel) { app.logout() }
                }
            }
            .navigationTitle("Zwei-Faktor")
        }
    }
}

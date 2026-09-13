package main

// Unter Windows gibt es (noch) keine Einzelinstanz-Erkennung: dort startet die App
// nicht über einen .desktop-Eintrag, ein versehentlicher Doppelstart ist also
// unwahrscheinlicher.

func notifyRunning() bool { return false }

func (a *app) listenSingleton() func() { return func() {} }

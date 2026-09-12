using HelloExq;

// exq run run-app -- <name> から渡された引数をそのまま受け取る。
var name = args.Length > 0 ? args[0] : null;
Console.WriteLine(Greeter.Greet(name));

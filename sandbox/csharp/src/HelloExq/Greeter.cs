namespace HelloExq;

/// <summary>
/// exq の利用例のためだけに存在する最小のロジック。
/// テスト対象を持たせることで test / test-filter コマンドの例が成立する。
/// </summary>
public static class Greeter
{
    /// <summary>名前つきの挨拶を返す。空や空白のみの場合は "world" にフォールバックする。</summary>
    public static string Greet(string? name)
    {
        var target = string.IsNullOrWhiteSpace(name) ? "world" : name.Trim();
        return $"Hello, {target}!";
    }
}
